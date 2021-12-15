package rtc

import (
	"context"
	"crypto/tls"
	"io"
	"log"

	"github.com/lucas-clemente/quic-go"
)

type Sender struct {
	session quic.Session
	media   io.Reader
}

func NewSender(media io.Reader, addr string) (*Sender, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	session, err := quic.DialAddr(addr, tlsConf, &quic.Config{EnableDatagrams: true})
	if err != nil {
		return nil, err
	}
	return &Sender{
		session: session,
		media:   media,
	}, nil
}

func (s *Sender) Run(ctx context.Context) error {
	buf := make([]byte, 1200)

	defer func() {
		log.Println("closing sender")
		s.session.CloseWithError(0, "eos")
	}()

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
