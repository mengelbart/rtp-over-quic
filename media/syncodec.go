package media

import (
	"log"

	"github.com/mengelbart/syncodec"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

const (
	H264 = "h264"
	VP8  = "vp8"
	VP9  = "vp9"
)

type SyncodecSource struct {
	Config

	targetBitrate uint
	codec         syncodec.Codec
	rtpWriter     interceptor.RTPWriter
	packetizer    rtp.Packetizer
}

func NewSyncodecSource(rtpWriter interceptor.RTPWriter, opts ...ConfigOption) (*SyncodecSource, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	payloader, err := payloaderForCodec(c.codec)
	if err != nil {
		return nil, err
	}
	packetizer := rtp.NewPacketizer(
		c.payloadType,
		c.ssrc,
		payloader,
		rtp.NewRandomSequencer(),
		c.clockRate,
	)
	s := &SyncodecSource{
		Config:        *c,
		targetBitrate: 0,
		codec:         nil,
		rtpWriter:     rtpWriter,
		packetizer:    packetizer,
	}
	codec, err := syncodec.NewStatisticalEncoder(s, syncodec.WithInitialTargetBitrate(int(s.targetBitrate)))
	if err != nil {
		return nil, err
	}
	s.codec = codec
	return s, nil
}

func (e *SyncodecSource) WriteFrame(frame syncodec.Frame) {
	samples := uint32(frame.Duration.Seconds() * float64(e.clockRate))
	pkts := e.packetizer.Packetize(e.mtu, frame.Content, samples)
	for _, pkt := range pkts {
		if _, err := e.rtpWriter.Write(&pkt.Header, pkt.Payload, nil); err != nil {
			log.Printf("WARNING: failed to write RTP packet: %v", err)
		}
	}
}

func (s *SyncodecSource) Play() error {
	go s.codec.Start()
	return nil
}

func (s *SyncodecSource) Stop() error {
	return s.codec.Close()
}

func (s *SyncodecSource) SetTargetBitsPerSecond(r uint) {
	s.codec.SetTargetBitrate(int(r))
}

type SyncodecSink struct{}

func NewSyncodecSink() (*SyncodecSink, error) {
	return &SyncodecSink{}, nil
}

func (s *SyncodecSink) Write(b []byte) (int, error) {
	return len(b), nil
}

func (s *SyncodecSink) Stop() error {
	return nil
}

func (s *SyncodecSink) Play() error {
	return nil
}
