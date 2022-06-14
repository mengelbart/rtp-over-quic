package media

import (
	"fmt"

	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

type GstreamerSource struct {
	Config
	src       string
	pipeline  *gstsrc.Pipeline
	rtpWriter interceptor.RTPWriter
}

func NewGstreamerSource(rtpWriter interceptor.RTPWriter, src string, opts ...ConfigOption) (*GstreamerSource, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("invalid source string: %v, use 'videotestsrc' or a valid filename instead", src)
	}

	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	s := &GstreamerSource{
		Config:    *c,
		src:       src,
		rtpWriter: rtpWriter,
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
	s.pipeline.SetSSRC(uint(s.ssrc))
	s.pipeline.SetBitRate(s.targetBitrate)
	return s, nil
}

func (s *GstreamerSource) Play() error {
	go gstsrc.StartMainLoop()
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

func (s *GstreamerSource) Stop() {
	s.pipeline.Stop()
	s.pipeline.Destroy()
}

func (s *GstreamerSource) SetTargetBitrate(bitrate uint) {
	s.pipeline.SetBitRate(bitrate)
}
