package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mengelbart/rtp-over-quic/controller"
	"github.com/mengelbart/rtp-over-quic/media"
	"github.com/spf13/cobra"
)

var (
	sink         string
	rtcpFeedback string
)

func init() {
	rootCmd.AddCommand(receiveCmd)

	receiveCmd.Flags().StringVar(&sink, "sink", "autovideosink", "Media sink")
	receiveCmd.Flags().StringVar(&rtcpFeedback, "rtcp-feedback", "none", "RTCP Congestion Control Feedback to send ('none', 'rfc8888', 'rfc8888-pion', 'twcc')")
}

type RTCPFeedback int

func (f RTCPFeedback) String() string {
	switch f {
	case RTCP_NONE:
		return "none"
	case RTCP_RFC8888:
		return "rfc8888"
	case RTCP_RFC8888_PION:
		return "rfc8888-pion"
	case RTCP_TWCC:
		return "twcc"
	default:
		log.Printf("WARNING: unknown RTCP Congestion Control Feedback type: %v, using default ('none')\n", int(f))
		return "none"
	}
}

func getRTCP(choice string) RTCPFeedback {
	switch choice {
	case "none":
		return RTCP_NONE
	case "rfc8888":
		return RTCP_RFC8888
	case "rfc8888-pion":
		return RTCP_RFC8888_PION
	case "twcc":
		return RTCP_TWCC
	default:
		log.Printf("WARNING: unknown RTCP Congestion Control Feedback type: %v, using default ('none')\n", choice)
		return RTCP_NONE
	}
}

const (
	RTCP_NONE RTCPFeedback = iota
	RTCP_RFC8888
	RTCP_RFC8888_PION
	RTCP_TWCC
)

var receiveCmd = &cobra.Command{
	Use: "receive",
	Run: func(_ *cobra.Command, _ []string) {
		if err := startReceiver(); err != nil {
			log.Fatal(err)
		}
	},
}

func startReceiver() error {
	mediaOptions := []media.ConfigOption{
		media.Codec(codec),
	}
	mediaFactory := GstreamerSinkFactory(sink, mediaOptions...)
	if sink == "syncodec" {
		mediaFactory = SyncodecSinkFactory()
	}
	options := []controller.Option[controller.BaseServer]{
		controller.SetAddr[controller.BaseServer](addr),
		controller.SetRTPLogFileName[controller.BaseServer](rtpDumpFile),
		controller.SetRTCPLogFileName[controller.BaseServer](rtcpDumpFile),
		controller.SetQLOGDirName[controller.BaseServer](qlogDir),
		controller.SetSSLKeyLogFileName[controller.BaseServer](keyLogFile),
	}
	switch getRTCP(rtcpFeedback) {
	case RTCP_RFC8888:
		options = append(options, controller.EnableRFC8888())
	case RTCP_RFC8888_PION:
		options = append(options, controller.EnableRFC8888Pion())
	case RTCP_TWCC:
		options = append(options, controller.EnableTWCC())
	}

	var s Starter
	s, err := getServer(transport, mediaFactory, options...)
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

func getServer(transport string, mf controller.MediaSinkFactory, options ...controller.Option[controller.BaseServer]) (Starter, error) {
	switch transport {
	case "quic", "quic-dgram":
		return controller.NewQUICServer(mf, false, options...)
	case "quic-stream":
		options = append(options, controller.MTU[controller.BaseServer](65_000))
		return controller.NewQUICServer(mf, true, options...)
	case "udp":
		return controller.NewUDPServer(mf, options...)
	case "tcp":
		return controller.NewTCPServer(mf, options...)
	}
	return nil, errInvalidTransport
}

type mediaSinkFactoryFunc func() (controller.MediaSink, error)

func (f mediaSinkFactoryFunc) Create() (controller.MediaSink, error) {
	return f()
}

func GstreamerSinkFactory(sink string, opts ...media.ConfigOption) controller.MediaSinkFactory {
	return mediaSinkFactoryFunc(func() (controller.MediaSink, error) {
		return media.NewGstreamerSink(sink, opts...)
	})
}

func SyncodecSinkFactory(opts ...media.Config) controller.MediaSinkFactory {
	return mediaSinkFactoryFunc(func() (controller.MediaSink, error) {
		return media.NewSyncodecSink()
	})
}
