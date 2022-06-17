package controller

import (
	"io"

	"github.com/pion/interceptor"
)

type MediaSource interface {
	Play() error
	Stop() error
	SetTargetBitrate(uint)
}

type MediaSourceFactory interface {
	Create(interceptor.RTPWriter) (MediaSource, error)
}

type MediaSink interface {
	io.Writer
	Play() error
	Stop() error
}

type MediaSinkFactory interface {
	Create() (MediaSink, error)
}
