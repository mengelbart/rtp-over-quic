package controller

import (
	"context"
	"errors"
	"io"
	"log"
	"net"

	"github.com/mengelbart/rtp-over-quic/transport"
	"golang.org/x/sync/errgroup"
)

type UDPSender struct {
	BaseSender
}

func NewUDPSender(media MediaSourceFactory, opts ...Option[BaseSender]) (*UDPSender, error) {
	bs, err := newBaseSender(media, opts...)
	if err != nil {
		return nil, err
	}
	return &UDPSender{BaseSender: *bs}, nil
}

func (s *UDPSender) Start(ctx context.Context) error {
	connection, err := connectUDP(s.addr)
	if err != nil {
		return err
	}
	t := transport.NewUDPTransportWithConn(connection)
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
		if _, err := connection.Write([]byte("bye")); err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("failed to write 'bye' message: %v", err)
			}
		}
		return connection.Close()
	})
	return wg.Wait()
}

func (s *UDPSender) readRTCPFromNetwork(transport io.Reader) error {
	buf := make([]byte, s.mtu)
	for {
		n, err := transport.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case s.rtcpChan <- rtcpFeedback{
			buf:        buf[:n],
			attributes: nil,
		}:
		default:
			log.Println("RTCP buffer full, dropping packet")
		}
	}
}
