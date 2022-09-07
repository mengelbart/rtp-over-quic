package tcp

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"

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

type ServerConfig struct {
	localAddr string
}

type Server struct {
	*ServerConfig
	onNewHandler func(*Handler)
}

func NewServer(opts ...ServerOption) (*Server, error) {
	s := &Server{
		ServerConfig: &ServerConfig{
			localAddr: ":4242",
		},
		onNewHandler: nil,
	}
	for _, opt := range opts {
		if err := opt(s.ServerConfig); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Server) OnNewHandler(f func(*Handler)) {
	s.onNewHandler = f
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := listenTCP(s.localAddr)
	if err != nil {
		return err
	}
	log.Printf("listening on %v...", listener.Addr())

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
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

type pkt struct {
	buffer []byte
}

type Handler struct {
	reader interceptor.RTPReader
	conn   *net.TCPConn
}

func (h *Handler) SetRTPReader(r interceptor.RTPReader) {
	h.reader = r
}

func (h *Handler) handle(ctx context.Context, conn *net.TCPConn) error {
	pktChan := make(chan pkt)

	var wg sync.WaitGroup
	defer wg.Wait()

	go h.receive(pktChan)

	for {
		select {
		case p := <-pktChan:
			if h.reader != nil {
				h.reader.Read(p.buffer, interceptor.Attributes{})
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (h *Handler) receive(pktChan chan<- pkt) error {
	prefix := make([]byte, 2)
	for {
		if _, err := io.ReadFull(h.conn, prefix); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			log.Printf("failed to read length from TCP conn: %v, exiting", err)
			return err
		}
		length := binary.BigEndian.Uint16(prefix)
		buf := make([]byte, length)
		if _, err := io.ReadFull(h.conn, buf); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			log.Printf("failed to read complete frame from TCP conn: %v, exiting", err)
			return err
		}
		pktChan <- pkt{
			buffer: buf,
		}
	}
}

func (h *Handler) WriteRTCP(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
	buf, err := rtcp.Marshal(pkts)
	if err != nil {
		return 0, err
	}
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(buf)))
	return h.conn.Write(append(length, buf...))
}
