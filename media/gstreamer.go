package media

import (
	"fmt"
	"io"
	"log"
	"sync"

	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

var srcMainLoop sync.Once
var sinkMainLoop sync.Once

type GstreamerSource struct {
	Config
	src       string
	pipeline  *gstsrc.Pipeline
	rtpWriter interceptor.RTPWriter
}

func NewGstreamerSource(rtpWriter interceptor.RTPWriter, src string, opts ...ConfigOption) (*GstreamerSource, error) {
	srcMainLoop.Do(func() {
		go gstsrc.StartMainLoop()
	})
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
	p, err := gstsrc.NewPipeline(s.codec, srcString, gstsrc.MTU(c.mtu))
	if err != nil {
		return nil, err
	}
	s.pipeline = p
	s.pipeline.SetSSRC(uint(s.ssrc))
	s.pipeline.SetBitRate(s.targetBitrate)
	log.Printf("src pipeline: %v", s.pipeline.String())
	return s, nil
}

func (s *GstreamerSource) Play() error {
	go s.pipeline.Start()

	buf := make([]byte, s.mtu)
	for {
		n, err := s.pipeline.Read(buf)
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				return nil
			}
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

func (s *GstreamerSource) Stop() error {
	return s.pipeline.Close()
}

func (s *GstreamerSource) SetTargetBitrate(bitrate uint) {
	s.pipeline.SetBitRate(bitrate)
}

type GstreamerSink struct {
	Config
	io.Writer
	dst      string
	pipeline *gstsink.Pipeline
}

func NewGstreamerSink(dst string, opts ...ConfigOption) (*GstreamerSink, error) {
	sinkMainLoop.Do(func() {
		go gstsink.StartMainLoop()
	})
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	s := &GstreamerSink{
		Config: *c,
		dst:    dst,
	}
	p, err := gstsink.NewPipeline(s.codec, s.dst)
	if err != nil {
		return nil, err
	}
	s.pipeline = p
	log.Printf("sink pipeline: %v", s.pipeline.String())
	return s, nil
}

func (s *GstreamerSink) Play() error {
	go s.pipeline.Start()
	return nil
}

func (s *GstreamerSink) Stop() error {
	return s.pipeline.Close()
}

func (s *GstreamerSink) Write(buf []byte) (int, error) {
	return s.pipeline.Write(buf)
}
