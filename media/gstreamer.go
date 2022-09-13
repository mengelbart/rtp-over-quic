package media

import (
	"fmt"
	"io"
	"log"
	"math"
	"time"

	"github.com/mengelbart/gst-go/gstreamer"
	"github.com/mengelbart/rtp-over-quic/rtp"
	"github.com/pion/interceptor"
	pionrtp "github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media/h264reader"
)

type mtuGetter interface {
	getMTU(gstreamer.Buffer) uint
}

// TODO: If usefule, make this configurable?
const teeLiveVideo = false // if set, displays source video in autovideosink

type GstreamerSource struct {
	Config
	src              string
	pipeline         *gstreamer.Pipeline
	rtpWriter        interceptor.RTPWriter
	useGstPacketizer bool
	close            chan struct{}
}

func NewGstreamerSource(rtpWriter interceptor.RTPWriter, src string, useGstPacketizer bool, opts ...ConfigOption) (*GstreamerSource, error) {
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
	builder = append(builder,
		gstreamer.NewElement("clocksync"),
	)

	if teeLiveVideo {
		builder = append(builder,
			gstreamer.NewElement("tee", gstreamer.Set("name", "t")),
			gstreamer.NewElement("queue"),
			gstreamer.NewElement("autovideosink t."),
			gstreamer.NewElement("queue"),
		)
	}

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
			gstreamer.Set("cpu-used", 4),
			gstreamer.Set("deadline", 1),
		))
		if useGstPacketizer {
			builder = append(builder, gstreamer.NewElement(fmt.Sprintf("rtp%vpay", c.codec), payloaderSettings...))
		}
	case "h264":
		builder = append(builder, gstreamer.NewElement("x264enc",
			gstreamer.Set("name", "encoder"),
			gstreamer.Set("pass", 5),
			gstreamer.Set("speed-preset", 4),
			gstreamer.Set("tune", 4),
			// gstreamer.Set("key-int-max", 10),
		))
		if useGstPacketizer {
			builder = append(builder, gstreamer.NewElement("rtph264pay", payloaderSettings...))
		}
	case "h265":
		builder = append(builder, gstreamer.NewElement("x265enc"))
		if useGstPacketizer {
			builder = append(builder, gstreamer.NewElement("rtph265pay", payloaderSettings...))
		}
	case "av1":
		panic("rtpav1pay is not yet implemented in Gstreamer")
		//builder = append(builder, gstreamer.NewElement("av1enc"))
		//if useGstPacketizer {
		//builder = append(builder, gstreamer.NewElement("rtpav1pay", payloaderSettings...))
		//}
	}

	builder = append(builder,
		gstreamer.NewElement("appsink", gstreamer.Set("name", "appsink")),
	)
	pipelineStr := builder.Build()
	log.Printf("src pipeline: %v", pipelineStr)

	pipeline, err := gstreamer.NewPipeline(pipelineStr)
	if err != nil {
		return nil, err
	}
	s := &GstreamerSource{
		Config:           *c,
		src:              src,
		pipeline:         pipeline,
		rtpWriter:        rtpWriter,
		useGstPacketizer: useGstPacketizer,
		close:            make(chan struct{}),
	}
	return s, nil
}

func (s *GstreamerSource) Play() error {
	bufferCh := make(chan gstreamer.Buffer)
	s.pipeline.SetBufferHandler(func(b gstreamer.Buffer) {
		bufferCh <- b
	})
	s.pipeline.SetEOSHandler(func() {
		close(bufferCh)
	})
	s.pipeline.SetErrorHandler(func(err error) {
		panic(fmt.Errorf("ERROR: %w", err))
		// TODO
	})
	go s.pipeline.Start()

	var packetizer pionrtp.Packetizer
	if !s.useGstPacketizer {
		payloader, err := payloaderForCodec(s.codec)
		if err != nil {
			return err
		}
		packetizer = pionrtp.NewPacketizer(s.payloadType, s.ssrc, payloader, pionrtp.NewFixedSequencer(0), 90_000)
	}

	for {
		select {
		case <-s.close:
			return nil
		case buffer, ok := <-bufferCh:
			if !ok {
				return nil
			}
			if !s.useGstPacketizer {
				samples := uint32((time.Duration(buffer.Duration).Seconds()) * float64(s.clockRate))

				attributes := interceptor.Attributes{
					rtp.RELIABILITY: rtp.NOT_REQUIRED,
				}
				mtu := s.mtu
				if isKeyFrame(s.codec, buffer.Bytes) {
					attributes.Set(rtp.RELIABILITY, rtp.REQUIRED)
					mtu = math.MaxUint16
				}

				pkts := packetizer.Packetize(mtu, buffer.Bytes, samples)
				for _, pkt := range pkts {
					_, err := s.rtpWriter.Write(&pkt.Header, pkt.Payload, attributes)
					if err != nil {
						log.Printf("rtpWriter.Write error: %v", err)
						return err
					}
				}
			} else {
				var pkt pionrtp.Packet
				err := pkt.Unmarshal(buffer.Bytes)
				if err != nil {
					return err
				}
				_, err = s.rtpWriter.Write(&pkt.Header, pkt.Payload, nil)
				if err != nil {
					log.Printf("rtpWriter.Write error: %v", err)
					return err
				}
			}
		}
	}
}

func isKeyFrame(codec string, buffer []byte) bool {
	switch codec {
	case "vp8":
		return buffer[0]&0x1 == 0
	case "h264":
		r, w := io.Pipe()
		hr, err := h264reader.NewReader(r)
		if err != nil {
			panic(err)
		}
		go func() {
			_, verr := w.Write(buffer)
			if verr != nil {
				panic(verr)
			}
			r.Close()
		}()
		hasIDR := false
		nal, err := hr.NextNAL()
		for nal != nil && err != io.EOF {
			if err != nil {
				panic(err)
			}
			nal, err = hr.NextNAL()
			if nal != nil {
				if nal.UnitType == h264reader.NalUnitTypeCodedSliceIdr {
					hasIDR = true
				}
			}
		}

		return hasIDR
	}
	return false
}

func (s *GstreamerSource) Stop() error {
	close(s.close)
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
		gstreamer.NewElement("clocksync"),
		gstreamer.NewElement("videorate"),
	)

	if teeLiveVideo {
		builder = append(builder,
			gstreamer.NewElement("tee", gstreamer.Set("name", "t")),
			gstreamer.NewElement("queue"),
			gstreamer.NewElement("autovideosink t."),
			gstreamer.NewElement("queue"),
		)
	}

	if dst == "autovideosink" {
		builder = append(builder,
			gstreamer.NewElement("autovideosink"),
		)
	} else {
		builder = append(builder,
			gstreamer.NewElement("y4menc"),
			gstreamer.NewElement(fmt.Sprintf("filesink location=%v", dst)),
		)
	}

	pipelineStr := builder.Build()
	log.Printf("sink pipeline: %v", pipelineStr)

	pipeline, err := gstreamer.NewPipeline(pipelineStr)
	if err != nil {
		return nil, err
	}
	s := &GstreamerSink{
		Config:   *c,
		Writer:   pipeline,
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
