package controller

import (
	"log"
	"net"

	"github.com/mengelbart/rtp-over-quic/transport"
)

type UDPSender struct {
	baseSender

	connection *net.UDPConn
	transport  *transport.UDP
}

func NewUDPSender(media MediaSourceFactory, opts ...Option) (*UDPSender, error) {
	bs, err := newBaseController(media, opts...)
	if err != nil {
		return nil, err
	}
	connection, err := connectUDP(bs.addr)
	if err != nil {
		return nil, err
	}
	s := &UDPSender{
		connection: connection,
		transport:  transport.NewUDPTransportWithConn(connection),
		baseSender: *bs,
	}

	s.flow.Bind(s.transport)

	return s, nil
}

func (s *UDPSender) Start() error {
	go func() {
		if err := s.readRTCPFromNetwork(); err != nil {
			log.Printf("failed to read from network: %v", err)
		}
	}()
	return s.baseSender.Start()
}

func (s *UDPSender) readRTCPFromNetwork() error {
	buf := make([]byte, 1500)
	for {
		n, err := s.transport.Read(buf)
		if err != nil {
			if e, ok := err.(net.Error); ok && !e.Temporary() {
				log.Printf("failed to read from transport: %v (non-temporary, exiting read routine)", err)
				return err
			}
			log.Printf("failed to read from transport: %v", err)
		}
		s.rtcpChan <- rtcpFeedback{
			buf:        buf[:n],
			attributes: nil,
		}
	}
}
