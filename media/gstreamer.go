package media

import (
	"fmt"

	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

type GstreamerSource struct {
	codec         string
	src           string
	ssrc          uint
	targetBitrate uint
	pipeline      *gstsrc.Pipeline
	rtpWriter     interceptor.RTPWriter
}

type GstreamerSourceOption func(*GstreamerSource) error

func NewGstreamerSource(rtpWriter interceptor.RTPWriter, opts ...GstreamerSourceOption) (*GstreamerSource, error) {
	s := &GstreamerSource{
		codec:         "h264",
		src:           "videotestsrc",
		ssrc:          0,
		targetBitrate: 100_000,
		rtpWriter:     rtpWriter,
	}

	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}

	var srcString string
	if s.src != "videotestsrc" {
		srcString = fmt.Sprintf("filesrc location=%v ! decodebin ! clocksync ", s.src)
	} else {
		srcString = "videotestsrc ! clocksync "
	}
	p, err := gstsrc.NewPipeline(s.codec, srcString)
	if err != nil {
		return nil, err
	}
	s.pipeline = p
	s.pipeline.SetSSRC(s.ssrc)
	s.pipeline.SetBitRate(s.targetBitrate)
	return s, nil
}

func (s *GstreamerSource) Play() error {
	go s.pipeline.Start()

	buf := make([]byte, 2<<16)
	for {
		n, err := s.pipeline.Read(buf)
		if err != nil {
			return err
		}
		var pkt rtp.Packet
		err = pkt.Unmarshal(buf[:n])
		if err != nil {
			return err
		}
		_, err = s.rtpWriter.Write(&pkt.Header, pkt.Payload, nil)
		if err != nil {
			return err
		}
	}
}

func (s *GstreamerSource) SetTargetBitrate(bitrate uint) {
	s.pipeline.SetBitRate(bitrate)
}

func GstreamerSourceCodec(c string) GstreamerSourceOption {
	return func(gs *GstreamerSource) error {
		gs.codec = c
		return nil
	}
}

func GstreamerSourceString(s string) GstreamerSourceOption {
	return func(gs *GstreamerSource) error {
		gs.src = s
		return nil
	}
}

func GstreamerSourceSSRC(ssrc uint) GstreamerSourceOption {
	return func(gs *GstreamerSource) error {
		gs.ssrc = ssrc
		return nil
	}
}

func GstreamerSourceInitialTargetBitrate(r uint) GstreamerSourceOption {
	return func(gs *GstreamerSource) error {
		gs.targetBitrate = r
		return nil
	}
}
