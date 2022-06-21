package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/mengelbart/rtp-over-quic/transport"
	"golang.org/x/sync/errgroup"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type rtcpFeedback struct {
	buf        []byte
	attributes interceptor.Attributes
}

type TransportMode int

const (
	DGRAM TransportMode = iota
	STREAM
	PRIORITIZED
)

type QUICSender struct {
	BaseSender
	TransportMode
}

func NewQUICSender(media MediaSourceFactory, mode TransportMode, opts ...Option[BaseSender]) (*QUICSender, error) {
	bs, err := newBaseSender(media, append(opts, EnableFlowID(0))...)
	if err != nil {
		return nil, err
	}
	return &QUICSender{
		BaseSender:    *bs,
		TransportMode: mode,
	}, nil
}

func (s *QUICSender) Start(ctx context.Context) error {
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
	var t io.ReadWriter

	switch s.TransportMode {
	case STREAM:
		t = transport.NewStreamTransportWithConn(connection)
		s.flow.Bind(t)
	case DGRAM:
		t = transport.NewDgramTransportWithConn(connection)
		s.flow.Bind(t)
	case PRIORITIZED:
		st := transport.NewStreamTransportWithConn(connection)
		dt := transport.NewDgramTransportWithConn(connection)
		t = st
		s.flow.BindPriority(0, 0, st)
		s.flow.BindPriority(1, 0, dt)
		s.flow.SetPrioritizer(transport.PrioritizerFunc(func(_ *rtp.Header, b []byte) int {
			vp9Pkt := codecs.VP9Packet{}
			if _, err := vp9Pkt.Unmarshal(b); err != nil {
				panic(err)
			}
			if vp9Pkt.P {
				return 1
			}
			return 0
		}))
	default:
		return fmt.Errorf("unknown transport mode: %v", s.TransportMode)
	}

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

func (s *QUICSender) readRTCPFromNetwork(transport io.Reader) error {
	buf := make([]byte, s.mtu)
	for {
		n, err := transport.Read(buf)
		if err != nil {
			if e, ok := err.(*quic.ApplicationError); ok && e.ErrorCode == 0 {
				return nil
			}
			return err
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
