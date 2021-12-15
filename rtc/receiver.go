package rtc

import (
	"context"
	"io"
	"log"

	"github.com/lucas-clemente/quic-go"
	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
)

type Receiver struct {
	session quic.Session
	media   io.WriteCloser
}

func CreateGstreamerReceiver(session quic.Session) (*Receiver, error) {
	dstPipeline, err := gstsink.NewPipeline("vp8", "autovideosink")
	if err != nil {
		return nil, err
	}
	dstPipeline.Start()
	return newReceiver(dstPipeline, session)
}

func newReceiver(media io.WriteCloser, session quic.Session) (*Receiver, error) {
	return &Receiver{
		session: session,
		media:   media,
	}, nil
}

func (r *Receiver) run(ctx context.Context) error {
	defer func() {
		log.Println("closing receiver")
		r.session.CloseWithError(0, "eos")
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			buf, err := r.session.ReceiveMessage()
			if err != nil {
				return err
			}
			_, err = r.media.Write(buf)
			if err != nil {
				return err
			}
		}
	}
}

func (r *Receiver) Close() error {
	return r.media.Close()
}
