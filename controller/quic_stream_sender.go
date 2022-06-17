package controller

import (
	"bytes"
	"context"
	"io"
	"log"

	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/mengelbart/rtp-over-quic/transport"
	"golang.org/x/sync/errgroup"
)

type QUICStreamSender struct {
	BaseSender
}

func NewQUICStreamSender(media MediaSourceFactory, opts ...Option[BaseSender]) (*QUICStreamSender, error) {
	bs, err := newBaseSender(media, append(opts, EnableFlowID(0))...)
	if err != nil {
		return nil, err
	}
	s := &QUICStreamSender{
		BaseSender: *bs,
	}
	return s, nil
}

func (s *QUICStreamSender) Start(ctx context.Context) error {
	connection, err := connectQUIC(
		ctx,
		s.addr,
		s.quicCC,
		nil,
		s.qlogDirName,
		s.sslKeyLogFileName,
	)
	if err != nil {
		return err
	}
	t := transport.NewStreamTransportWithConn(connection)
	s.flow.Bind(t)

	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		return s.readRTCPFromNetwork(t)
	})

	wg.Go(func() error {
		return s.BaseSender.Start(ctx)
	})

	return wg.Wait()
}

func (s *QUICStreamSender) readRTCPFromNetwork(transport io.Reader) error {
	buf := make([]byte, 1500)
	for {
		n, err := transport.Read(buf)
		if err != nil {
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
