package udp

import (
	"context"
	"errors"
	"log"
	"net"

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
		SenderConfig: &SenderConfig{remoteAddr: ""},
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
	buf := make([]byte, 1500) // TODO: Better MTU?
	for {
		n, err := s.conn.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				log.Printf("Exiting reader routine: UDP socket error: %v", err)
				return
			}
			log.Printf("failed to receive UDP datagram: %v", err)
			continue
		}
		select {
		case rtcpChan <- rtp.RTCPFeedback{
			Buffer:     buf[:n],
			Attributes: nil,
		}:
		case <-ctx.Done():
		default:
			log.Println("RTCP buffer full, dropping packet")
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
			return s.conn.Write(append(headerBuf, payload...))
		},
	))
}
