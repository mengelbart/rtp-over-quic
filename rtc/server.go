package rtc

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
)

type ReceiverFactory func(quic.Session) (*Receiver, error)

type Server struct {
	wg           sync.WaitGroup
	listener     quic.Listener
	makeReceiver ReceiverFactory
}

func NewServer(f ReceiverFactory, addr string) (*Server, error) {
	quicConf := &quic.Config{
		EnableDatagrams: true,
		MaxIdleTimeout:  time.Second,
	}

	listener, err := quic.ListenAddr(addr, generateTLSConfig(), quicConf)
	if err != nil {
		return nil, err
	}

	return &Server{
		wg:           sync.WaitGroup{},
		listener:     listener,
		makeReceiver: f,
	}, nil
}

func (s *Server) Listen(ctx context.Context) (err error) {
	s.wg.Add(1)

	defer func() {
		log.Println("closing server")
		s.wg.Done()
	}()

	for {
		session, err := s.listener.Accept(ctx)
		if err != nil {
			return err
		}
		go s.receiveStreamLoop(ctx, session)
		receiver, err := s.makeReceiver(session)
		if err != nil {
			log.Printf("failed to create receiver: %v\n", err)
			continue
		}
		go func() {
			defer receiver.Close()
			err := receiver.run(ctx)
			if err != nil {
				log.Printf("receiver closed connection: %v\n", err)
			}
		}()
	}
}

func (s *Server) receiveStreamLoop(ctx context.Context, session quic.Session) error {
	fmt.Println("Accept stream")
	stream, err := session.AcceptUniStream(ctx)
	if err != nil {
		return err
	}
	fmt.Println("got stream")
	buf := make([]byte, 1200)
	for {
		n, err := stream.Read(buf)
		if err != nil {
			return err
		}
		log.Printf("received %v stream bytes\n", n)
	}
}

func (s *Server) Close() error {
	defer s.wg.Wait()
	return s.listener.Close()
}
