package rtc

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/logging"
)

type ReceiverFactory func(quic.Session, MediaSinkFactory) (*Receiver, error)

type MediaSinkFactory func() (MediaSink, error)

type Server struct {
	wg           sync.WaitGroup
	listener     quic.Listener
	makeReceiver ReceiverFactory
	sinkFactory  MediaSinkFactory
}

func NewServer(f ReceiverFactory, addr string, sinkFactory MediaSinkFactory, tracer logging.Tracer) (*Server, error) {
	quicConf := &quic.Config{
		EnableDatagrams: true,
		MaxIdleTimeout:  time.Second,
		Tracer:          tracer,
	}

	listener, err := quic.ListenAddr(addr, generateTLSConfig(), quicConf)
	if err != nil {
		return nil, err
	}

	return &Server{
		wg:           sync.WaitGroup{},
		listener:     listener,
		makeReceiver: f,
		sinkFactory:  sinkFactory,
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

		receiver, err := s.makeReceiver(session, s.sinkFactory)
		if err != nil {
			log.Printf("failed to create receiver: %v\n", err)
			continue
		}
		go func() {
			log.Println("starting receiver")
			defer receiver.Close()
			err := receiver.run(ctx)
			if err != nil {
				log.Printf("receiver closed connection: %v\n", err)
			}
		}()
	}
}

func (s *Server) receiveStreamLoop(ctx context.Context, session quic.Session) error {
	log.Println("Accept stream")
	defer log.Println("Exiting receive stream loop")
	stream, err := session.AcceptUniStream(ctx)
	if err != nil {
		return err
	}
	log.Println("got stream")
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
	defer log.Println("Receiver closed")
	defer s.wg.Wait()
	return s.listener.Close()
}
