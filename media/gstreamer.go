package media

import (
	"fmt"
	"io"
	"log"

	"github.com/mengelbart/gst-go/gstreamer"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

type GstreamerSource struct {
	Config
	src       string
	pipeline  *gstreamer.Pipeline
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
	builder := gstreamer.Elements{}

	if src == "videotestsrc" {
		builder = append(builder,
			gstreamer.NewElement("videotestsrc"),
		)
	} else {
		builder = append(builder,
			gstreamer.NewElement("filesrc", gstreamer.Set("location", src)),
			gstreamer.NewElement("decodebin"),
		)
	}
	builder = append(builder, gstreamer.NewElement("clocksync"))

	payloaderSettings := []gstreamer.ElementOption{
		gstreamer.Set("name", "payloader"),
		gstreamer.Set("mtu", c.mtu),
		gstreamer.Set("seqnum-offset", 0),
		gstreamer.Set("ssrc", c.ssrc),
	}
	// TODO: Set encoder options including init target bitrate
	switch c.codec {
	case "vp8", "vp9":
		builder = append(builder, gstreamer.NewElement(fmt.Sprintf("%venc", c.codec),
			gstreamer.Set("name", "encoder"),
			gstreamer.Set("error-resilient", "default"),
			gstreamer.Set("keyframe-max-dist", 300),
			gstreamer.Set("cpu-used", 4),
			gstreamer.Set("deadline", 1),
		))
		builder = append(builder, gstreamer.NewElement(fmt.Sprintf("rtp%vpay", c.codec), payloaderSettings...))
	case "h264":
		builder = append(builder, gstreamer.NewElement("x264enc",
			gstreamer.Set("name", "encoder"),
			gstreamer.Set("pass", 5),
			gstreamer.Set("speed-preset", 4),
			gstreamer.Set("tune", 4),
		))
		builder = append(builder, gstreamer.NewElement("rtph264pay", payloaderSettings...))
	case "h265":
		builder = append(builder, gstreamer.NewElement("x265enc"))
		builder = append(builder, gstreamer.NewElement("rtph265pay", payloaderSettings...))
	case "av1":
		panic("rtpav1pay is not yet implemented in Gstreamer")
		//builder = append(builder, gstreamer.NewElement("av1enc"))
		//builder = append(builder, gstreamer.NewElement("rtpav1pay", payloaderSettings...))
	}

	builder = append(builder, gstreamer.NewElement("appsink", gstreamer.Set("name", "appsink")))
	pipelineStr := builder.Build()
	log.Printf("src pipeline: %v", pipelineStr)

	pipeline, err := gstreamer.NewPipeline(pipelineStr, func() {}, func(err error) {})
	if err != nil {
		return nil, err
	}
	s := &GstreamerSource{
		Config:    *c,
		src:       src,
		pipeline:  pipeline,
		rtpWriter: rtpWriter,
	}
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

func (s *GstreamerSource) SetTargetBitsPerSecond(bitrate uint) {
	value := bitrate
	prop := "bitrate"
	switch s.codec {
	case "vp8", "vp9":
		prop = "target-bitrate"
	case "h264":
		value = value / 1000
	}
	s.pipeline.SetPropertyUint("encoder", prop, value)
}

type GstreamerSink struct {
	Config
	io.Writer
	dst      string
	pipeline *gstreamer.Pipeline
}

func NewGstreamerSink(dst string, opts ...ConfigOption) (*GstreamerSink, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}

	builder := gstreamer.Elements{
		gstreamer.NewElement("appsrc", gstreamer.Set("name", "src")),
	}

	jitterBufferSettings := []gstreamer.ElementOption{}

	switch c.codec {
	case "vp8":
		builder = append(builder,
			gstreamer.NewElement("application/x-rtp, encoding-name=VP8-DRAFT-IETF-01"),
			gstreamer.NewElement("rtpjitterbuffer", jitterBufferSettings...),
			gstreamer.NewElement("rtpvp8depay"),
		)
	case "vp9":
		builder = append(builder,
			gstreamer.NewElement("application/x-rtp, encoding-name=VP9-DRAFT-IETF-01"),
			gstreamer.NewElement("rtpjitterbuffer", jitterBufferSettings...),
			gstreamer.NewElement("rtpvp9depay"),
		)
	case "h264":
		builder = append(builder,
			gstreamer.NewElement("application/x-rtp"),
			gstreamer.NewElement("rtpjitterbuffer", jitterBufferSettings...),
			gstreamer.NewElement("rtph264depay"),
		)
	case "h265":
		builder = append(builder,
			gstreamer.NewElement("application/x-rtp"),
			gstreamer.NewElement("rtpjitterbuffer", jitterBufferSettings...),
			gstreamer.NewElement("rtph265depay"),
		)
	case "av1":
		panic("rtpav1depay is not yet implemented in Gstreamer")
		//builder = append(builder,
		//	gstreamer.NewElement("rtpjitterbuffer", jitterBufferSettings...),
		//	gstreamer.NewElement("rtpav1depay"),
		//)
	}

	builder = append(builder,
		gstreamer.NewElement("decodebin"),
		gstreamer.NewElement("videoconvert"),
		gstreamer.NewElement("autovideosink"),
	)

	pipelineStr := builder.Build()
	log.Printf("sink pipeline: %v", pipelineStr)

	pipeline, err := gstreamer.NewPipeline(pipelineStr, func() {}, func(err error) {})
	if err != nil {
		return nil, err
	}
	s := &GstreamerSink{
		Config:   *c,
		Writer:   pipeline,
		dst:      dst,
		pipeline: pipeline,
	}
	return s, nil
}

func (s *GstreamerSink) Play() error {
	go s.pipeline.Start()
	return nil
}

func (s *GstreamerSink) Stop() error {
	return s.pipeline.Close()
}
