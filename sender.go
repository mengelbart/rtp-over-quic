package main

import (
	"context"
	"crypto/tls"
	"io"

	"github.com/lucas-clemente/quic-go"
)

type sender struct {
	session quic.Session
	media   io.Reader
}

func newSender(media io.Reader) (*sender, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	session, err := quic.DialAddr(addr, tlsConf, &quic.Config{EnableDatagrams: true})
	if err != nil {
		return nil, err
	}
	return &sender{
		session: session,
		media:   media,
	}, nil
}

func (s *sender) run(ctx context.Context) error {
	buf := make([]byte, 1200)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			n, err := s.media.Read(buf)
			if err != nil {
				return err
			}
			if err := s.session.SendMessage(buf[:n]); err != nil {
				return err
			}
		}
	}
}
