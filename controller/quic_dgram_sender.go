package controller

import (
	"log"
	"net"

	"github.com/lucas-clemente/quic-go"
	"github.com/mengelbart/rtp-over-quic/transport"

	"github.com/pion/interceptor"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type rtcpFeedback struct {
	buf        []byte
	attributes interceptor.Attributes
}

type QUICDgramSender struct {
	baseSender

	conn      quic.Connection
	transport *transport.Dgram
}

func NewQUICDgramSender(media MediaSourceFactory, opts ...Option) (*QUICDgramSender, error) {
	bs, err := newBaseSender(media, opts...)
	if err != nil {
		return nil, err
	}
	var connection quic.Connection
	var tracer *RTTTracer
	if bs.localRFC8888 {
		tracer = NewTracer()
		connection, err = connectQUIC(
			bs.addr,
			bs.quicCC,
			tracer,
			bs.qlogDirName,
			bs.sslKeyLogFileName,
		)
	} else {
		connection, err = connectQUIC(
			bs.addr,
			bs.quicCC,
			nil,
			bs.qlogDirName,
			bs.sslKeyLogFileName,
		)
	}
	if err != nil {
		return nil, err
	}
	s := &QUICDgramSender{
		conn:       connection,
		transport:  transport.NewDgramTransportWithConn(connection),
		baseSender: *bs,
	}

	s.flow.Bind(s.transport)

	if s.localRFC8888 {
		s.flow.EnableLocalFeedback(0, tracer, func(f transport.Feedback) {
			s.rtcpChan <- rtcpFeedback{
				buf:        f.Buf,
				attributes: map[interface{}]interface{}{"timestamp": f.Timestamp},
			}
		})
	}

	return s, nil
}

func (s *QUICDgramSender) Start() error {
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
	if s.stream {
		go func() {
			if err := streamSendLoop(s.conn); err != nil {
				errCh <- err
			}
		}()
	}
	err := <-errCh
	return err
}

func (s *QUICDgramSender) readRTCPFromNetwork() error {
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

func (s *QUICDgramSender) Close() error {
	if err := s.flow.Close(); err != nil {
		return err
	}
	if err := s.baseSender.Close(); err != nil {
		return err
	}
	return s.conn.CloseWithError(0, "bye")
}

func streamSendLoop(connection quic.Connection) error {
	log.Println("Open stream")
	stream, err := connection.OpenUniStream()
	if err != nil {
		return err
	}
	log.Println("Opened stream")
	buf := make([]byte, 1200)
	for {
		_, err := stream.Write(buf)
		if err != nil {
			return err
		}
	}
}
