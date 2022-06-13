package controller

import (
	"log"
	"net"

	"github.com/lucas-clemente/quic-go"
	"github.com/mengelbart/rtp-over-quic/transport"
)

type QUICStreamSender struct {
	baseSender

	conn      quic.Connection
	transport *transport.Stream
}

func NewQUICStreamSender(media MediaSourceFactory, opts ...Option) (*QUICStreamSender, error) {
	bs, err := newBaseController(media, opts...)
	if err != nil {
		return nil, err
	}
	connection, err := connectQUIC(
		bs.addr,
		bs.quicCC,
		nil,
		bs.qlogDirName,
		bs.sslKeyLogFileName,
	)
	if err != nil {
		return nil, err
	}
	s := &QUICStreamSender{
		baseSender: *bs,
		conn:       connection,
		transport:  transport.NewStreamTransportWithConn(connection),
	}
	s.flow.Bind(s.transport)
	return s, nil
}

func (s *QUICStreamSender) Start() error {
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

func (s *QUICStreamSender) readRTCPFromNetwork() error {
	buf := make([]byte, 1500)
	for {
		n, err := s.transport.Read(buf)
		if err != nil {
			if e, ok := err.(net.Error); ok && !e.Temporary() {
				log.Printf("failed to read from transport: %v (non-temporary, exiting read routine)", err)
				return err
			}
			if e, ok := err.(*quic.ApplicationError); ok && e.ErrorCode == 0 {
				log.Printf("got application error code 0, exiting")
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

func (s *QUICStreamSender) Close() error {
	if err := s.flow.Close(); err != nil {
		return err
	}
	if err := s.baseSender.Close(); err != nil {
		return err
	}
	return s.conn.CloseWithError(0, "bye")
}
