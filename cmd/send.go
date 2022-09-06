package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mengelbart/rtp-over-quic/cc"
	"github.com/mengelbart/rtp-over-quic/controller"
	"github.com/mengelbart/rtp-over-quic/media"
	"github.com/mengelbart/rtp-over-quic/quic"
	"github.com/mengelbart/rtp-over-quic/rtp"
	"github.com/mengelbart/rtp-over-quic/scream"
	"github.com/mengelbart/rtp-over-quic/tcp"
	"github.com/mengelbart/rtp-over-quic/udp"
	"github.com/pion/interceptor"
	rtpcc "github.com/pion/interceptor/pkg/cc"
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
		if err := start(cmd.Context()); err != nil {
			log.Fatal(err)
		}
	},
}

func setupInterceptor() (interceptor.Interceptor, error) {
	rtpOptions := []rtp.Option{
		rtp.RegisterSenderPacketLog(rtpDumpFile, rtcpDumpFile),
	}

	if rtpCC == controller.SCReAM.String() {
		rtpOptions = append(rtpOptions, rtp.RegisterSCReAM(func(id string, estimator scream.BandwidthEstimator) {
			// TODO
		}, int(initialTargetBitrate)))
	}
	if rtpCC == controller.GCC.String() {
		rtpOptions = append(rtpOptions, rtp.RegisterTWCCHeaderExtension())
		rtpOptions = append(rtpOptions, rtp.RegisterGCC(func(id string, estimator rtpcc.BandwidthEstimator) {
			// TODO
		}))
	}
	return rtp.New(rtpOptions...)
}

func start(ctx context.Context) error {
	in, err := setupInterceptor()
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
	return startMedia(sender)
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
	return nil, fmt.Errorf("unknown transport: %v", transport)
}

func startQUICSender(ctx context.Context, in interceptor.Interceptor) (interceptor.RTPWriter, error) {
	sender, err := quic.NewSender(
		in,
		quic.RemoteAddress(addr),
		quic.SetQLOGDirName(qlogDir),
		quic.SetSSLKeyLogFileName(keyLogFile),
		quic.SetQUICCongestionControlAlgorithm(cc.AlgorithmFromString(quicCC)),
	)
	if err != nil {
		return nil, err
	}
	if err := sender.Connect(ctx); err != nil {
		return nil, err
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
	return sender, nil
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
	return sender, nil
}

func startMedia(writer interceptor.RTPWriter) error {
	mediaOptions := []media.ConfigOption{
		media.Codec(codec),
		media.InitialTargetBitrate(initialTargetBitrate),
	}
	source, err := media.NewGstreamerSource(writer, source, transport != "quic-prio", mediaOptions...)
	if err != nil {
		return err
	}
	return source.Play()
}

func startSender() error {
	mediaOptions := []media.ConfigOption{
		media.Codec(codec),
		media.InitialTargetBitrate(initialTargetBitrate),
	}
	if transport == "quic-stream" {
		mediaOptions = append(mediaOptions, media.MTU(1_000_000))
	}
	mediaFactory := GstreamerSourceFactory(source, transport != "quic-prio", mediaOptions...)
	if source == "syncodec" {
		mediaFactory = SyncodecSourceFactory(mediaOptions...)
	}
	options := []controller.Option[controller.BaseSender]{
		controller.SetAddr[controller.BaseSender](addr),
		controller.SetRTPLogFileName[controller.BaseSender](rtpDumpFile),
		controller.SetRTCPLogFileName[controller.BaseSender](rtcpDumpFile),
		controller.SetCCLogFileName(ccDump),
		controller.SetQLOGDirName[controller.BaseSender](qlogDir),
		controller.SetSSLKeyLogFileName[controller.BaseSender](keyLogFile),
		controller.SetQUICCongestionControlAlgorithm[controller.BaseSender](controller.CongestionControlAlgorithmFromString(quicCC)),
		controller.SetTCPCongestionControlAlgorithm[controller.BaseSender](controller.CongestionControlAlgorithmFromString(tcpCongAlg)),
		controller.SetRTPCongestionControlAlgorithm(controller.CongestionControlAlgorithmFromString(rtpCC)),
		controller.InitialRate(int(initialTargetBitrate)),
	}
	if sendStream {
		options = append(options, controller.EnableStream[controller.BaseSender]())
	}
	if localRFC8888 {
		options = append(options, controller.EnableLocalRFC8888())
	}

	var s Starter
	s, err := getSender(transport, mediaFactory, options...)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigs)
		cancel()
	}()
	go func() {
		select {
		case <-sigs:
			cancel()
		case <-ctx.Done():
		}
	}()
	return s.Start(ctx)
}

type Starter interface {
	Start(ctx context.Context) error
}

func getSender(transport string, mf controller.MediaSourceFactory, options ...controller.Option[controller.BaseSender]) (Starter, error) {
	switch transport {
	case "quic", "quic-dgram":
		return controller.NewQUICSender(mf, controller.DGRAM, options...)
	case "quic-stream":
		options = append(options, controller.MTU[controller.BaseSender](65_000))
		return controller.NewQUICSender(mf, controller.STREAM, options...)
	case "quic-prio":
		return controller.NewQUICSender(mf, controller.PRIORITIZED, options...)
	case "udp":
		return controller.NewUDPSender(mf, options...)
	case "tcp":
		return controller.NewTCPSender(mf, options...)
	}
	return nil, errInvalidTransport
}

type mediaSourceFactoryFunc func(interceptor.RTPWriter) (controller.MediaSource, error)

func (f mediaSourceFactoryFunc) Create(w interceptor.RTPWriter) (controller.MediaSource, error) {
	return f(w)
}

func GstreamerSourceFactory(src string, useGstPacketizer bool, opts ...media.ConfigOption) controller.MediaSourceFactory {
	return mediaSourceFactoryFunc(func(w interceptor.RTPWriter) (controller.MediaSource, error) {
		return media.NewGstreamerSource(w, src, useGstPacketizer, opts...)
	})
}

func SyncodecSourceFactory(opts ...media.ConfigOption) controller.MediaSourceFactory {
	return mediaSourceFactoryFunc(func(w interceptor.RTPWriter) (controller.MediaSource, error) {
		return media.NewSyncodecSource(w, opts...)
	})
}
