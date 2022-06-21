package controller

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/mengelbart/rtp-over-quic/scream"
	"github.com/mengelbart/rtp-over-quic/transport"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"golang.org/x/sync/errgroup"
)

type BaseSender struct {
	// config
	commonBaseConfig
	rtpCC             CongestionControlAlgorithm
	initialTargetRate int
	localRFC8888      bool

	ccLogFileName string

	// implementation
	registry  *interceptor.Registry
	ccLogFile io.WriteCloser

	// TODO: Bundle flow and media and store as list to allow multiple streams
	flow         *transport.RTPFlow
	mediaFactory MediaSourceFactory
	media        MediaSource

	bweChan chan scream.BandwidthEstimator

	rtcpChan chan rtcpFeedback
}

func newBaseSender(media MediaSourceFactory, opts ...Option[BaseSender]) (*BaseSender, error) {
	c := &BaseSender{
		commonBaseConfig: commonBaseConfig{
			addr:              ":4242",
			mtu:               1500,
			quicCC:            Reno,
			qlogDirName:       "",
			sslKeyLogFileName: "",
			tcpCC:             Cubic,
			stream:            false,
			rtpLogFileName:    "",
			rtcpLogFileName:   "",
		},
		rtpCC:             NONE,
		initialTargetRate: 100_000,
		localRFC8888:      false,
		ccLogFileName:     "",
		registry:          &interceptor.Registry{},
		ccLogFile:         nil,
		flow:              transport.NewRTPFlow(),
		mediaFactory:      media,
		media:             nil,
		bweChan:           make(chan scream.BandwidthEstimator),
		rtcpChan:          make(chan rtcpFeedback),
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	if len(c.rtpLogFileName) > 0 || len(c.rtcpLogFileName) > 0 {
		if err := c.registerSenderPacketLog(c.registry); err != nil {
			return nil, err
		}
	}
	if c.rtpCC == SCReAM {
		if err := registerSCReAM(c.registry, c.onNewSCrEAMEstimator, c.initialTargetRate); err != nil {
			return nil, err
		}
	}
	if c.rtpCC == GCC {
		if err := registerTWCCHeaderExtension(c.registry); err != nil {
			return nil, err
		}
		if err := registerGCC(c.registry, c.OnBandwidthEstimationUpdate); err != nil {
			return nil, err
		}
	}
	ccLogFile, err := getLogFile(c.ccLogFileName)
	if err != nil {
		return nil, err
	}
	c.ccLogFile = ccLogFile
	return c, nil
}

func (s *BaseSender) onNewSCrEAMEstimator(_ string, bwe scream.BandwidthEstimator) {
	go func() {
		s.bweChan <- bwe
	}()
}

func (s *BaseSender) Start(ctx context.Context) error {
	i, err := s.registry.Build("")
	if err != nil {
		return err
	}
	defer func() {
		if err1 := i.Close(); err1 != nil {
			log.Printf("failed to close interceptor: %v", err1)
		}
	}()

	writer := i.BindLocalStream(&interceptor.StreamInfo{
		ID:                  "",
		Attributes:          map[interface{}]interface{}{},
		SSRC:                0,
		PayloadType:         0,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{{URI: transportCCURI, ID: 1}},
		MimeType:            "",
		ClockRate:           0,
		Channels:            0,
		SDPFmtpLine:         "",
		RTCPFeedback: []interceptor.RTCPFeedback{{
			Type:      "ack",
			Parameter: "ccfb",
		}},
	}, s.flow)
	m, err := s.mediaFactory.Create(writer)
	if err != nil {
		return err
	}
	s.media = m
	reader := i.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		s.runBandwidthEstimation(ctx)
		return nil
	})

	g.Go(func() error {
		s.readRTCP(ctx, reader)
		return nil
	})

	g.Go(func() error {
		if err := s.media.Play(); err != nil {
			return err
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		return s.media.Stop()
	})

	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

func (s *BaseSender) readRTCP(ctx context.Context, reader interceptor.RTCPReader) {
	for {
		select {
		case report := <-s.rtcpChan:
			_, _, err := reader.Read(report.buf, report.attributes)
			if err != nil {
				log.Printf("RTCP reader returned error: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *BaseSender) OnBandwidthEstimationUpdate(id string, bwe cc.BandwidthEstimator) {
	target := bwe.GetTargetBitrate()
	if target < 0 {
		log.Printf("got negative target bitrate: %v", target)
		return
	}
	s.media.SetTargetBitrate(uint(target))
}

func (s *BaseSender) runBandwidthEstimation(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)

	var bwe scream.BandwidthEstimator
	select {
	case bwe = <-s.bweChan:
	case <-ctx.Done():
		return
	}

	for {
		select {
		case bwe = <-s.bweChan:
		case now := <-ticker.C:
			target, err := bwe.GetTargetBitrate(0)
			if err != nil {
				log.Printf("got error on bwe.GetTargetBitrate: %v", err)
				continue
			}
			if target < 0 {
				log.Printf("[SCReAM] got negative target bitrate: %v", target)
				continue
			}
			stats := bwe.GetStats()
			fmt.Fprintf(
				s.ccLogFile, "%v, %v, %v, %v, %v, %v, %v, %v, %v, %v\n",
				now.UnixMilli(),
				target,
				stats["queueDelay"],
				stats["sRTT"],
				stats["cwnd"],
				stats["bytesInFlightLog"],
				stats["rateLostStream0"],
				stats["rateTransmittedStream0"],
				stats["rateAckedStream0"],
				stats["hiSeqAckStream0"],
			)
			s.media.SetTargetBitrate(uint(target))
		case <-ctx.Done():
			return
		}
	}
}
