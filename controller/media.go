package controller

import "github.com/pion/interceptor"

type MediaSource interface {
	Play() error
	Stop()
	SetTargetBitrate(uint)
}

type MediaSourceFactory interface {
	Create(interceptor.RTPWriter) (MediaSource, error)
}
