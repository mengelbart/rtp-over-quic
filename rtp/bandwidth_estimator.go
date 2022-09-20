package rtp

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mengelbart/rtp-over-quic/logging"
	"github.com/mengelbart/rtp-over-quic/scream"
	"github.com/pion/interceptor/pkg/cc"
)

type Media interface {
	SetTargetBitsPerSecond(uint)
}

type BandwidthEstimator struct {
	media Media

	screamBWE chan scream.BandwidthEstimator
	gccBWE    chan cc.BandwidthEstimator

	logFile string
}

func NewBandwidthEstimator(logfile string) (*BandwidthEstimator, error) {
	return &BandwidthEstimator{
		media:     nil,
		screamBWE: make(chan scream.BandwidthEstimator),
		gccBWE:    make(chan cc.BandwidthEstimator),
		logFile:   logfile,
	}, nil
}

func (e *BandwidthEstimator) SetMedia(m Media) {
	e.media = m
}

func (e *BandwidthEstimator) OnNewSCReAMEstimator(_ string, bwe scream.BandwidthEstimator) {
	e.screamBWE <- bwe
}

func (e *BandwidthEstimator) OnNewGCCEstimator(_ string, bwe cc.BandwidthEstimator) {
	e.gccBWE <- bwe
}

func (e *BandwidthEstimator) RunGCC(ctx context.Context) error {
	panic("TODO")
}

func (e *BandwidthEstimator) RunSCReAM(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)

	ccLogFile, err := logging.GetLogFile(e.logFile)
	if err != nil {
		return err
	}
	defer ccLogFile.Close()

	log.Printf("waiting for bwe")
	var bwe scream.BandwidthEstimator
	select {
	case bwe = <-e.screamBWE:
	case <-ctx.Done():
		return nil
	}

	for {
		select {
		case bwe = <-e.screamBWE:
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
				ccLogFile, "%v, %v, %v, %v, %v, %v, %v, %v, %v, %v, %v\n",
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
				stats["isInFastStart"],
			)
			if e.media != nil {
				e.media.SetTargetBitsPerSecond(uint(target))
			}
		case <-ctx.Done():
			return nil
		}
	}
}
