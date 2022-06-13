package controller

import (
	"log"
	"net"

	"github.com/mengelbart/rtp-over-quic/transport"
)

type TCPSender struct {
	baseSender

	connection *net.TCPConn
	transport  *transport.TCP
}

func NewTCPSender(media MediaSourceFactory, opts ...Option) (*TCPSender, error) {
	bs, err := newBaseController(media, opts...)
	if err != nil {
		return nil, err
	}
	connection, err := connectTCP(bs.addr, bs.tcpCC)
	if err != nil {
		return nil, err
	}
	s := &TCPSender{
		connection: connection,
		transport:  transport.NewTCPTransportWithConn(connection),
		baseSender: *bs,
	}
	s.flow.Bind(s.transport)
	return s, nil
}

func (s *TCPSender) Start() error {
	errCh := make(chan error)
	go func() {
		if err := s.readRTCPFromNetwork(); err != nil {
			errCh <- err
		}
	}()
	go func() {
		if err := s.baseSender.Start(); err != nil {
			errCh <- err
		}
	}()
	err := <-errCh
	return err
}

func (s *TCPSender) readRTCPFromNetwork() error {
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

func (s *TCPSender) Close() error {
	if err := s.flow.Close(); err != nil {
		return err
	}
	if err := s.baseSender.Close(); err != nil {
		return err
	}
	return s.connection.Close()
}
