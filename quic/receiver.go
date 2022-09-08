package quic

import (
	"bytes"
	"context"
	"io"
	"log"
	"sync"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/mengelbart/rtp-over-quic/cc"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
)

type ServerOption func(*ServerConfig) error

func LocalAddress(addr string) ServerOption {
	return func(sc *ServerConfig) error {
		sc.localAddr = addr
		return nil
	}
}

func SetServerQLOGDirName(dir string) ServerOption {
	return func(sc *ServerConfig) error {
		sc.qlogDirectoryName = dir
		return nil
	}
}

func SetServerSSLKeyLogFileName(file string) ServerOption {
	return func(sc *ServerConfig) error {
		sc.sslKeyLogFileName = file
		return nil
	}
}

func SetServerQUICCongestionControlAlgorithm(algorithm cc.Algorithm) ServerOption {
	return func(sc *ServerConfig) error {
		sc.cc = algorithm
		return nil
	}
}

type ServerConfig struct {
	localAddr         string
	cc                cc.Algorithm
	qlogDirectoryName string
	sslKeyLogFileName string
}

type Server struct {
	*ServerConfig
	onNewHandler func(*Handler)
}

func NewServer(opts ...ServerOption) (*Server, error) {
	s := &Server{
		ServerConfig: &ServerConfig{
			localAddr:         "",
			cc:                0,
			qlogDirectoryName: "",
			sslKeyLogFileName: "",
		},
	}
	for _, opt := range opts {
		if err := opt(s.ServerConfig); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := listen(s.localAddr, s.cc, s.qlogDirectoryName, s.sslKeyLogFileName)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if err == context.Canceled {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := Handler{
				reader: nil,
				conn:   conn,
			}
			s.onNewHandler(&h)
			if err = h.handle(ctx, conn); err != nil {
				log.Printf("error on handling connection: %v", err)
			}
		}()
	}
}

type TransportMode int

const (
	DGRAM TransportMode = iota
	STREAM
	PRIORITIZED
)

func (s *Server) OnNewHandler(f func(*Handler)) {
	s.onNewHandler = f
}

type pkt struct {
	flowID    uint64
	transport TransportMode
	buffer    []byte
}

type Handler struct {
	reader interceptor.RTPReader
	conn   quic.Connection
}

func (h *Handler) SetRTPReader(r interceptor.RTPReader) {
	h.reader = r
}

func (h *Handler) handle(ctx context.Context, conn quic.Connection) error {
	pktChan := make(chan pkt)

	var wg sync.WaitGroup
	defer wg.Wait()

	go h.receiveDgrams(pktChan)
	go h.acceptStreams(ctx, pktChan)

	for {
		select {
		case p := <-pktChan:
			if h.reader != nil {
				if _, _, err := h.reader.Read(p.buffer, interceptor.Attributes{
					"flow-id":   p.flowID,
					"transport": p.transport,
				}); err != nil {
					log.Printf("failed to process incoming packet: %v", err)
				}
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func (h *Handler) receiveDgrams(pktChan chan<- pkt) {
	for {
		msg, err := h.conn.ReceiveMessage()
		if err != nil {
			panic("TODO") // TODO
		}
		id, err := quicvarint.Read(bytes.NewReader(msg))
		if err != nil {
			panic("TODO") // TODO
		}
		offset := quicvarint.Len(id)
		pktChan <- pkt{
			flowID:    id,
			transport: DGRAM,
			buffer:    msg[offset:],
		}

	}
}

func (h *Handler) acceptStreams(ctx context.Context, pktChan chan<- pkt) {
	for {
		stream, err := h.conn.AcceptUniStream(ctx)
		if err != nil {
			panic("TODO") // TODO
		}
		go h.readStream(stream, pktChan)
	}
}

func (h *Handler) readStream(stream quic.ReceiveStream, pktChan chan<- pkt) {
	varintReader := quicvarint.NewReader(stream)
	id, err := quicvarint.Read(varintReader)
	if err != nil {
		panic("TODO") // TODO
	}
	for {
		buf, err := io.ReadAll(stream)
		if err != nil {
			panic("TODO") // TODO
		}
		pktChan <- pkt{
			flowID:    id,
			transport: STREAM,
			buffer:    buf,
		}
	}
}

func (h *Handler) WriteRTCP(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
	buf, err := rtcp.Marshal(pkts)
	if err != nil {
		return 0, err
	}
	var id uint64
	if i := attributes.Get("flow-id"); i != nil {
		id = i.(uint64)
	}
	var idBuf bytes.Buffer
	idWriter := quicvarint.NewWriter(&idBuf)
	quicvarint.Write(idWriter, id)
	msg := append(idBuf.Bytes(), buf...)
	return len(buf), h.conn.SendMessage(msg, nil, nil)
}
