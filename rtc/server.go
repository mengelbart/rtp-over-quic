package rtc

import (
	"context"
	"log"

	"github.com/lucas-clemente/quic-go"
)

type receiverFactory func(quic.Session) (*Receiver, error)

type Server struct {
	listener     quic.Listener
	makeReceiver receiverFactory
}

func NewServer(f receiverFactory, addr string) (*Server, error) {
	quicConf := &quic.Config{
		EnableDatagrams: true,
	}

	listener, err := quic.ListenAddr(addr, generateTLSConfig(), quicConf)
	if err != nil {
		return nil, err
	}

	return &Server{
		listener:     listener,
		makeReceiver: f,
	}, nil
}

func (s *Server) Listen(ctx context.Context) (err error) {
	defer func() {
		err1 := s.listener.Close()
		if err != nil {
			return
		}
		err = err1
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
				log.Printf("receiver crashed: %v\n", err)
			}
		}()
	}
}
