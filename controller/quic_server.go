package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/mengelbart/rtp-over-quic/transport"
	"golang.org/x/sync/errgroup"
)

type QUICServer struct {
	BaseServer
	isStreamServer bool
}

func NewQUICServer(mediaFactory MediaSinkFactory, isStreamServer bool, opts ...Option[BaseServer]) (*QUICServer, error) {
	bs, err := newBaseServer(mediaFactory, opts...)
	if err != nil {
		return nil, err
	}
	return &QUICServer{
		BaseServer:     *bs,
		isStreamServer: isStreamServer,
	}, nil
}

func (s *QUICServer) Start(ctx context.Context) error {
	listener, err := listenQUIC(s.addr, s.quicCC, s.qlogDirName, s.sslKeyLogFileName)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if err == context.Canceled {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err = s.handle(ctx, conn); err != nil {
				log.Printf("error on handling connection: %v", err)
			}
		}()
	}
}

func (s *QUICServer) handle(ctx context.Context, conn quic.Connection) error {
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
	var t io.ReadWriter
	if s.isStreamServer {
		t = transport.NewStreamTransportWithConn(conn)
	}
	if !s.isStreamServer {
		t = transport.NewDgramTransportWithConn(conn)
	}
	receiver := newReceiver(s.mtu, demultiplexerFunc(func(pkt []byte) (uint64, []byte, error) {
		id, err1 := quicvarint.Read(bytes.NewReader(pkt))
		if err1 != nil {
			return 0, pkt, fmt.Errorf("failed to read flow ID: %w", err1)
		}
		offset := quicvarint.Len(id)
		return id, pkt[offset:], nil
	}))
	sink, err := s.mediaFactory.Create()
	if err != nil {
		return err
	}
	receiver.addIncomingFlow(0, i, sink, t)
	rtcpFlow := transport.NewRTCPFlowWithID(0)
	rtcpFlow.Bind(t)
	i.BindRTCPWriter(rtcpFlow)

	wg, ctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		defer func() {
			if err := sink.Stop(); err != nil {
				log.Printf("pipeline.Stop error: %v", err)
			}
		}()
		return receiver.receive()
	})
	wg.Go(sink.Play)

	select {
	case <-conn.Context().Done():
	case <-ctx.Done():
	}
	if err := conn.CloseWithError(quic.ApplicationErrorCode(0), "bye"); err != nil {
		log.Printf("error on closing connection: %v", err)
	}
	return wg.Wait()
}
