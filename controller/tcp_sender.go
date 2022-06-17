package controller

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/mengelbart/rtp-over-quic/transport"
	"golang.org/x/sync/errgroup"
)

type TCPSender struct {
	BaseSender
}

func NewTCPSender(media MediaSourceFactory, opts ...Option[BaseSender]) (*TCPSender, error) {
	bs, err := newBaseSender(media, opts...)
	if err != nil {
		return nil, err
	}
	return &TCPSender{
		BaseSender: *bs,
	}, nil
}

func (s *TCPSender) Start(ctx context.Context) error {
	connection, err := connectTCP(s.addr, s.tcpCC)
	if err != nil {
		return err
	}
	t := transport.NewTCPTransportWithConn(connection)
	s.flow.Bind(t)

	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		return s.readRTCPFromNetwork(t)
	})

	wg.Go(func() error {
		return s.BaseSender.Start(ctx)
	})

	wg.Go(func() error {
		<-ctx.Done()
		return connection.Close()
	})

	return wg.Wait()
}

func (s *TCPSender) readRTCPFromNetwork(transport io.Reader) error {
	buf := make([]byte, s.mtu)
	for {
		n, err := transport.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.rtcpChan <- rtcpFeedback{
			buf:        buf[:n],
			attributes: nil,
		}
	}
}
