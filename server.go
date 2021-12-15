package main

import (
	"context"
	"log"

	"github.com/lucas-clemente/quic-go"
)

type receiverFactory func(quic.Session) (*receiver, error)

type server struct {
	listener     quic.Listener
	makeReceiver receiverFactory
}

func newServer(f receiverFactory) (*server, error) {
	quicConf := &quic.Config{
		EnableDatagrams: true,
	}

	listener, err := quic.ListenAddr(addr, generateTLSConfig(), quicConf)
	if err != nil {
		return nil, err
	}

	return &server{
		listener:     listener,
		makeReceiver: f,
	}, nil
}

func (s *server) listen(ctx context.Context) error {
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
			err := receiver.run(ctx)
			if err != nil {
				log.Printf("receiver crashed: %v\n", err)
			}
		}()
	}
}
