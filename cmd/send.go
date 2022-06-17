package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mengelbart/rtp-over-quic/controller"
	"github.com/mengelbart/rtp-over-quic/media"
	"github.com/pion/interceptor"
	"github.com/spf13/cobra"
)

var (
	source string
	ccDump string
	rtpCC  string

	sendStream   bool
	localRFC8888 bool
)

func init() {
	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVar(&source, "source", "videotestsrc", "Media source")
	sendCmd.Flags().StringVar(&ccDump, "cc-dump", "", "Congestion Control log file, use 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&rtpCC, "rtp-cc", "none", "RTP congestion control algorithm. ('none', 'scream', 'gcc')")
	sendCmd.Flags().BoolVar(&localRFC8888, "local-rfc8888", false, "Generate local RFC 8888 feedback")
	sendCmd.Flags().BoolVar(&sendStream, "stream", false, "Send random data on a stream")
}

var sendCmd = &cobra.Command{
	Use: "send",
	Run: func(_ *cobra.Command, _ []string) {
		if err := startSender(); err != nil {
			log.Fatal(err)
		}
	},
}

func startSender() error {
	mediaOptions := []media.ConfigOption{
		media.Codec(codec),
	}
	if transport == "quic-stream" {
		mediaOptions = append(mediaOptions, media.MTU(65_000))
	}
	mediaFactory := GstreamerSourceFactory(source, mediaOptions...)
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
		return controller.NewQUICSender(mf, false, options...)
	case "quic-stream":
		options = append(options, controller.MTU[controller.BaseSender](65_000))
		return controller.NewQUICSender(mf, true, options...)
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

func GstreamerSourceFactory(src string, opts ...media.ConfigOption) controller.MediaSourceFactory {
	return mediaSourceFactoryFunc(func(w interceptor.RTPWriter) (controller.MediaSource, error) {
		return media.NewGstreamerSource(w, src, opts...)
	})
}

func SyncodecSourceFactory(opts ...media.ConfigOption) controller.MediaSourceFactory {
	return mediaSourceFactoryFunc(func(w interceptor.RTPWriter) (controller.MediaSource, error) {
		return media.NewSyncodecSource(w, opts...)
	})
}
