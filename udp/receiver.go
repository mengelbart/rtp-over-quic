package udp

import (
	"context"
	"errors"
	"log"
	"net"
	"net/netip"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
)

type ServerOption func(*ServerConfig) error

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
	conn, err := listenUDP(s.localAddr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		if err := conn.Close(); err != nil {
			log.Printf("failed to close UDP conn: %v", err)
		}
	}()

	handlers := make(map[netip.AddrPort]*Handler)
	for {
		buf := make([]byte, 1500) // TODO: Better/dynamic MTU?
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("ReadFromUDP error, exiting: %v", err)
			return err
		}
		handler, ok := handlers[addr.AddrPort()]
		if !ok {
			handler = &Handler{
				reader: nil,
				addr:   addr,
				conn:   conn,
			}
			handlers[addr.AddrPort()] = handler
			s.onNewHandler(handler)
		}
		handler.receive(pkt{
			buffer: buf[:n],
		})
	}
}

type pkt struct {
	buffer []byte
}

type Handler struct {
	reader interceptor.RTPReader
	addr   *net.UDPAddr
	conn   *net.UDPConn
}

func (h *Handler) SetRTPReader(r interceptor.RTPReader) {
	h.reader = r
}

func (h *Handler) receive(p pkt) {
	if _, _, err := h.reader.Read(p.buffer, interceptor.Attributes{}); err != nil {
		log.Printf("failed to process incoming packet: %v", err)
	}
}

func (h *Handler) WriteRTCP(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
	buf, err := rtcp.Marshal(pkts)
	if err != nil {
		return 0, err
	}
	return h.conn.WriteTo(buf, h.addr)
}
