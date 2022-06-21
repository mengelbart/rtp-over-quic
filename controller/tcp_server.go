package controller

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/mengelbart/rtp-over-quic/transport"
	"golang.org/x/sync/errgroup"
)

type TCPServer struct {
	BaseServer
}

func NewTCPServer(mediaFactory MediaSinkFactory, opts ...Option[BaseServer]) (*TCPServer, error) {
	bs, err := newBaseServer(mediaFactory, opts...)
	if err != nil {
		return nil, err
	}
	return &TCPServer{
		BaseServer: *bs,
	}, nil
}

func (s *TCPServer) Start(ctx context.Context) error {
	listener, err := listenTCP(s.addr)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	go func() {
		<-ctx.Done()
		if err := listener.Close(); err != nil {
			log.Printf("failed to close TCP listener: %v", err)
		}
	}()

	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.handle(ctx, conn); err != nil {
				log.Printf("failed to handle tcp conn: %v", err)
			}
		}()
	}
}

func (s *TCPServer) handle(ctx context.Context, conn *net.TCPConn) error {
	ir, err := s.buildInterceptorRegistry()
	if err != nil {
		return err
	}
	i, err := ir.Build(strconv.Itoa(0))
	if err != nil {
		return err
	}
	defer func() {
		if err1 := i.Close(); err1 != nil {
			log.Printf("failed to close interceptor: %v", err1)
		}
	}()
	tcp := transport.NewTCPTransportWithConn(conn)
	receiver := newReceiver(s.mtu, demultiplexerFunc(func(pkt []byte) (uint64, []byte, error) {
		return 0, pkt, nil
	}))
	sink, err := s.mediaFactory.Create()
	if err != nil {
		return err
	}
	receiver.addIncomingFlow(0, i, sink, []io.ReadWriter{tcp})
	rtcpFlow := transport.NewRTCPFlow()
	rtcpFlow.Bind(tcp)
	i.BindRTCPWriter(rtcpFlow)

	wg, ctx := errgroup.WithContext(ctx)

	wg.Go(func() error {
		defer func() {
			if err := sink.Stop(); err != nil {
				log.Printf("pipeline.Stop error: %v", err)
			}
		}()
		if err := receiver.receive(); err != nil {
			log.Printf("receive returned error: %v", err)
			return err
		}
		return nil
	})
	wg.Go(sink.Play)

	<-ctx.Done()
	if err := conn.Close(); err != nil {
		log.Printf("failed to close TCP conn: %v", err)
	}

	return wg.Wait()
}
