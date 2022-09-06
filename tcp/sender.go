package tcp

import (
	"context"
	"encoding/binary"
	"net"

	"github.com/mengelbart/rtp-over-quic/cc"
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

func SetTCPCongestionControlAlgorithm(algorithm cc.Algorithm) SenderOption {
	return func(sc *SenderConfig) error {
		sc.cc = algorithm
		return nil
	}
}

type SenderConfig struct {
	cc         cc.Algorithm
	remoteAddr string
}

type Sender struct {
	*SenderConfig

	conn        *net.TCPConn
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
	conn, err := connectTCP(s.remoteAddr, s.cc)
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
	msg := append(headerBuf, payload...)
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(msg)))
	return s.conn.Write(append(buf, msg...))
}

func (s *Sender) NewMediaStream() interceptor.RTPWriter {
	return s.interceptor.BindLocalStream(&interceptor.StreamInfo{}, s)
}
