package main

import (
	"context"
	"io"

	"github.com/lucas-clemente/quic-go"
	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
)

type receiver struct {
	session quic.Session
	media   io.Writer
}

func createGstreamerReceiver(session quic.Session) (*receiver, error) {
	dstPipeline, err := gstsink.NewPipeline("vp8", "autovideosink")
	if err != nil {
		return nil, err
	}
	dstPipeline.Start()
	return newReceiver(dstPipeline, session)
}

func newReceiver(media io.Writer, session quic.Session) (*receiver, error) {
	return &receiver{
		session: session,
		media:   media,
	}, nil
}

func (r *receiver) run(ctx context.Context) error {
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
