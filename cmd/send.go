package cmd

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/mengelbart/rtp-over-quic/cc"
	"github.com/mengelbart/rtp-over-quic/media"
	"github.com/mengelbart/rtp-over-quic/quic"
	"github.com/mengelbart/rtp-over-quic/rtp"
	"github.com/mengelbart/rtp-over-quic/tcp"
	"github.com/mengelbart/rtp-over-quic/udp"
	"github.com/pion/interceptor"
	"github.com/spf13/cobra"
)

var (
	source string
	ccDump string
	rtpCC  string

	sendStream           bool
	localRFC8888         bool
	initialTargetBitrate uint
)

func init() {
	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVar(&source, "source", "videotestsrc", "Media source")
	sendCmd.Flags().StringVar(&ccDump, "cc-dump", "", "Congestion Control log file, use 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&rtpCC, "rtp-cc", "none", "RTP congestion control algorithm. ('none', 'scream', 'gcc')")
	sendCmd.Flags().UintVar(&initialTargetBitrate, "target", 100_000, "Initial media target bitrate")
	sendCmd.Flags().BoolVar(&localRFC8888, "local-rfc8888", false, "Generate local RFC 8888 feedback")
	sendCmd.Flags().BoolVar(&sendStream, "stream", false, "Send random data on a stream")
}

var sendCmd = &cobra.Command{
	Use: "send",
	Run: func(cmd *cobra.Command, _ []string) {
		sc := senderController{}
		if err := sc.start(cmd.Context()); err != nil {
			log.Fatal(err)
		}
	},
}

type MediaSource interface {
	Play() error
	Stop() error
	SetTargetBitsPerSecond(uint)
}

type BandwidthEstimator interface {
	SetMedia(rtp.Media)
}

type senderController struct {
	bwe BandwidthEstimator
}

func (c *senderController) setupInterceptor(ctx context.Context) (interceptor.Interceptor, error) {
	rtpOptions := []rtp.Option{
		rtp.RegisterSenderPacketLog(rtpDumpFile, rtcpDumpFile),
	}

	if rtpCC == cc.SCReAM.String() {
		bwe, err := rtp.NewBandwidthEstimator(ccDump)
		if err != nil {
			return nil, err
		}
		c.bwe = bwe
		go func() {
			if err := bwe.RunSCReAM(ctx); err != nil {
				log.Printf("bwe.RunSCReAM returned error: %v", err)
			}
		}()
		rtpOptions = append(rtpOptions, rtp.RegisterSCReAM(bwe.OnNewSCReAMEstimator, int(initialTargetBitrate)))
	}
	if rtpCC == cc.GCC.String() {
		bwe, err := rtp.NewBandwidthEstimator(ccDump)
		if err != nil {
			return nil, err
		}
		c.bwe = bwe
		go func() {
			if err := bwe.RunGCC(ctx); err != nil {
				log.Printf("bwe.RunSCReAM returned error: %v", err)
			}
		}()
		rtpOptions = append(rtpOptions, rtp.RegisterTWCCHeaderExtension())
		rtpOptions = append(rtpOptions, rtp.RegisterGCC(bwe.OnNewGCCEstimator))
	}
	return rtp.New(rtpOptions...)
}

func (c *senderController) start(ctx context.Context) error {
	in, err := c.setupInterceptor(ctx)
	if err != nil {
		return err
	}
	senderFactory, err := transportFactory(transport)
	if err != nil {
		return err
	}
	sender, err := senderFactory(ctx, in)
	if err != nil {
		return err
	}
	return c.startMedia(sender)
}

func transportFactory(transport string) (func(context.Context, interceptor.Interceptor) (interceptor.RTPWriter, error), error) {
	switch transport {
	case "quic":
		return startQUICSender, nil
	case "udp":
		return startUDPSender, nil
	case "tcp":
		return startTCPSender, nil
	}
	return nil, errInvalidTransport
}

func startQUICSender(ctx context.Context, in interceptor.Interceptor) (interceptor.RTPWriter, error) {
	sender, err := quic.NewSender(
		in,
		quic.RemoteAddress(addr),
		quic.SetSenderQLOGDirName(qlogDir),
		quic.SetSenderSSLKeyLogFileName(keyLogFile),
		quic.SetSenderQUICCongestionControlAlgorithm(cc.AlgorithmFromString(quicCC)),
		quic.SetLocalRFC8888(localRFC8888),
	)
	if err != nil {
		return nil, err
	}
	if err := sender.Connect(ctx); err != nil {
		return nil, err
	}
	if sendStream {
		ds, err := sender.NewDataStream(ctx)
		if err != nil {
			return nil, err
		}
		go func() {
			rand.Seed(time.Now().UnixNano())
			buf := make([]byte, 1200)
			for {
				_, err := rand.Read(buf)
				if err != nil {
					log.Printf("failed to read random data, exiting data stream sender: %v", err)
					return
				}
				_, err = ds.Write(buf)
				if err != nil {
					log.Printf("failed to send random data, exiting data stream sender: %v", err)
					return
				}
			}
		}()
	}
	return sender.NewMediaStream(), nil
}

func startUDPSender(ctx context.Context, in interceptor.Interceptor) (interceptor.RTPWriter, error) {
	sender, err := udp.NewSender(
		in,
		udp.RemoteAddress(addr),
	)
	if err != nil {
		return nil, err
	}
	if err := sender.Connect(ctx); err != nil {
		return nil, err
	}
	return sender.NewMediaStream(), nil
}

func startTCPSender(ctx context.Context, in interceptor.Interceptor) (interceptor.RTPWriter, error) {
	sender, err := tcp.NewSender(
		in,
		tcp.RemoteAddress(addr),
	)
	if err != nil {
		return nil, err
	}
	if err := sender.Connect(ctx); err != nil {
		return nil, err
	}
	return sender.NewMediaStream(), nil
}

func (c *senderController) startMedia(writer interceptor.RTPWriter) error {
	mediaOptions := []media.ConfigOption{
		media.Codec(codec),
		media.InitialTargetBitrate(initialTargetBitrate),
	}
	var ms MediaSource
	var err error
	switch source {
	case "syncodec":
		ms, err = media.NewSyncodecSource(writer, mediaOptions...)
	default:
		ms, err = media.NewGstreamerSource(writer, source, transport != "quic-prio", mediaOptions...)
	}
	if err != nil {
		return err
	}
	if c.bwe != nil {
		c.bwe.SetMedia(ms)
	}
	return ms.Play()
}
