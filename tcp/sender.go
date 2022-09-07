package tcp

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"

	"github.com/mengelbart/rtp-over-quic/cc"
	"github.com/mengelbart/rtp-over-quic/rtp"
	"github.com/pion/interceptor"
	pionrtp "github.com/pion/rtp"
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

	rtcpReader := s.interceptor.BindRTCPReader(interceptor.RTCPReaderFunc(
		func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
			return len(b), a, nil
		}),
	)
	rtcpChan := make(chan rtp.RTCPFeedback)
	go rtp.ReadRTCP(ctx, rtcpReader, rtcpChan)
	go s.readFromNetwork(ctx, rtcpChan)

	return nil
}

func (s *Sender) readFromNetwork(ctx context.Context, rtcpChan chan rtp.RTCPFeedback) {
	buf := make([]byte, 1500) // TODO: Better MTU size?
	for {
		prefix := make([]byte, 2)
		if _, err := io.ReadFull(s.conn, prefix); err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("failed to read length from TCP conn: %v, exiting", err)
			continue
		}
		length := binary.BigEndian.Uint16(prefix)
		tmp := make([]byte, length)
		if _, err := io.ReadFull(s.conn, tmp); err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("failed to read complete frame from TCP conn: %v, exiting", err)
			continue
		}
		n := copy(buf, tmp)
		if n < int(length) {
			log.Printf("WARNING: buffer too short, TCP reader dropped %v bytes", int(length)-n)
		}
		rtcpChan <- rtp.RTCPFeedback{
			Buffer:     buf[:n],
			Attributes: nil,
		}
	}
}

func (s *Sender) NewMediaStream() interceptor.RTPWriter {
	return s.interceptor.BindLocalStream(&interceptor.StreamInfo{}, interceptor.RTPWriterFunc(
		func(header *pionrtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
			headerBuf, err := header.Marshal()
			if err != nil {
				return 0, err
			}
			msg := append(headerBuf, payload...)
			buf := make([]byte, 2)
			binary.BigEndian.PutUint16(buf[0:2], uint16(len(msg)))
			return s.conn.Write(append(buf, msg...))
		},
	))
}
