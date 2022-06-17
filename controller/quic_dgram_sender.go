package controller

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/mengelbart/rtp-over-quic/transport"
	"golang.org/x/sync/errgroup"

	"github.com/pion/interceptor"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type rtcpFeedback struct {
	buf        []byte
	attributes interceptor.Attributes
}

type QUICDgramSender struct {
	BaseSender
}

func NewQUICDgramSender(media MediaSourceFactory, opts ...Option[BaseSender]) (*QUICDgramSender, error) {
	bs, err := newBaseSender(media, append(opts, EnableFlowID(0))...)
	if err != nil {
		return nil, err
	}
	return &QUICDgramSender{
		BaseSender: *bs,
	}, nil
}

func (s *QUICDgramSender) Start(ctx context.Context) error {
	var connection quic.Connection
	var tracer *RTTTracer
	var err error
	if s.localRFC8888 {
		tracer = NewTracer()
		connection, err = connectQUIC(
			ctx,
			s.addr,
			s.quicCC,
			tracer,
			s.qlogDirName,
			s.sslKeyLogFileName,
		)
	} else {
		connection, err = connectQUIC(
			ctx,
			s.addr,
			s.quicCC,
			nil,
			s.qlogDirName,
			s.sslKeyLogFileName,
		)
	}
	if err != nil {
		return err
	}
	t := transport.NewDgramTransportWithConn(connection)
	s.flow.Bind(t)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		// TODO: Cancel on ctx.Done()
		return s.readRTCPFromNetwork(t)
	})
	g.Go(func() error {
		return s.BaseSender.Start(ctx)
	})

	if s.localRFC8888 {
		s.flow.RunLocalFeedback(ctx, 0, tracer, func(f transport.Feedback) {
			s.rtcpChan <- rtcpFeedback{
				buf:        f.Buf,
				attributes: map[interface{}]interface{}{"timestamp": f.Timestamp},
			}
		})
	}

	if s.stream {
		g.Go(func() error {
			// TODO: Cancel on ctx.Done()
			return streamSendLoop(connection)
		})
	}

	g.Go(func() error {
		<-ctx.Done()
		return connection.CloseWithError(0, "bye")
	})

	return g.Wait()
}

func (s *QUICDgramSender) readRTCPFromNetwork(transport io.Reader) error {
	buf := make([]byte, 1500)
	for {
		n, err := transport.Read(buf)
		if err != nil {
			if e, ok := err.(net.Error); ok && !e.Temporary() {
				return err
			}
			if e, ok := err.(*quic.ApplicationError); ok && e.ErrorCode == 0 {
				return nil
			}
			log.Printf("failed to read from transport: %v", err)
		}
		// TODO: If multiple RTCP flows are required, demultiplex on id here
		id, err := quicvarint.Read(bytes.NewReader(buf[:n]))
		if err != nil {
			log.Printf("failed to read flow ID: %v, dropping datagram", err)
			continue
		}
		s.rtcpChan <- rtcpFeedback{
			buf:        buf[quicvarint.Len(id):n],
			attributes: nil,
		}
	}
}

func streamSendLoop(connection quic.Connection) error {
	stream, err := connection.OpenUniStream()
	if err != nil {
		return err
	}
	buf := make([]byte, 1200)
	for {
		_, err := stream.Write(buf)
		if err != nil {
			return err
		}
	}
}
