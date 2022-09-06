package udp

import (
	"context"
	"net"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

type SenderOption func(*SenderConfig) error

func RemoteAddress(addr string) SenderOption {
	return func(sc *SenderConfig) error {
		sc.remoteAddr = addr
		return nil
	}
}

type SenderConfig struct {
	remoteAddr string
}

type Sender struct {
	*SenderConfig

	conn        *net.UDPConn
	interceptor interceptor.Interceptor
}

func NewSender(i interceptor.Interceptor, opts ...SenderOption) (*Sender, error) {
	s := &Sender{
		SenderConfig: &SenderConfig{},
		conn:         nil,
		interceptor:  i,
	}
	for _, opt := range opts {
		if err := opt(s.SenderConfig); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Sender) Connect(ctx context.Context) error {
	conn, err := connectUDP(s.remoteAddr)
	if err != nil {
		return err
	}
	s.conn = conn
	return nil
}

func (s *Sender) Write(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
	headerBuf, err := header.Marshal()
	if err != nil {
		return 0, err
	}
	return s.conn.Write(append(headerBuf, payload...))
}

func (s *Sender) NewMediaStream() interceptor.RTPWriter {
	return s.interceptor.BindLocalStream(&interceptor.StreamInfo{}, s)
}
