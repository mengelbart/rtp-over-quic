package media

import (
	"fmt"
	"log"
	"strings"

	"github.com/mengelbart/syncodec"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
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

func payloaderForCodec(codec string) (rtp.Payloader, error) {
	switch strings.ToLower(codec) {
	case strings.ToLower(H264):
		return &codecs.H264Payloader{}, nil
	case strings.ToLower(VP8):
		return &codecs.VP8Payloader{
			EnablePictureID: true,
		}, nil
	case strings.ToLower(VP9):
		return &codecs.VP9Payloader{}, nil
	default:
		return nil, fmt.Errorf("no payloader for codec %v available", codec)
	}
}

func NewSyncodecSource(rtpWriter interceptor.RTPWriter, opts ...ConfigOption) (*SyncodecSource, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	packetizer := rtp.NewPacketizer(
		uint16(c.mtu),
		c.payloadType,
		c.ssrc,
		&codecs.VP8Payloader{},
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
	pkts := e.packetizer.Packetize(frame.Content, samples)
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

func (s *SyncodecSource) Stop() {
	if err := s.codec.Close(); err != nil {
		log.Printf("WARNING: error on closing codec: %v", err)
	}
}

func (s *SyncodecSource) SetTargetBitrate(r uint) {
	s.codec.SetTargetBitrate(int(r))
}
