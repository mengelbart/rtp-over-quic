package rtc

import (
	"context"
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

func (s *Server) Close() error {
	defer s.wg.Wait()
	return s.listener.Close()
}
