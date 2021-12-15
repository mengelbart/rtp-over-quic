package main

import (
	"context"

	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
)

const addr = "localhost:4242"

func init() {
	go gstsink.StartMainLoop()
	go gstsrc.StartMainLoop()
}

func main() {
	go func() {
		err := startServer()
		if err != nil {
			panic(err)
		}
	}()

	if err := startSender(); err != nil {
		panic(err)
	}
}

func startServer() error {
	server, err := newServer(createGstreamerReceiver)
	if err != nil {
		return err
	}
	return server.listen(context.Background())
}

func startSender() error {
	srcPipeline, err := gstsrc.NewPipeline("vp8", "videotestsrc")
	if err != nil {
		return err
	}
	s, err := newSender(srcPipeline)
	if err != nil {
		return err
	}
	go srcPipeline.Start()
	return s.run(context.Background())
}
