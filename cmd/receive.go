package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mengelbart/rtp-over-quic/controller"
	"github.com/mengelbart/rtp-over-quic/media"
	"github.com/mengelbart/rtp-over-quic/quic"
	"github.com/mengelbart/rtp-over-quic/rtp"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/spf13/cobra"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type Starter interface {
	Start(ctx context.Context) error
}

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
	Run: func(cmd *cobra.Command, _ []string) {
		if err := start(cmd.Context()); err != nil {
			log.Fatal(err)
		}
	},
}

func start(ctx context.Context) error {
	server, err := quic.NewServer(
		quic.LocalAddress(addr),
		quic.SetServerQLOGDirName(qlogDir),
		quic.SetServerSSLKeyLogFileName(keyLogFile),
	)
	if err != nil {
		return err
	}
	mediaOptions := []media.ConfigOption{
		media.Codec(codec),
	}
	rtpOptions := []rtp.Option{
		rtp.RegisterReceiverPacketLog(rtpDumpFile, rtcpDumpFile),
	}
	switch getRTCP(rtcpFeedback) {
	case RTCP_RFC8888:
		rtpOptions = append(rtpOptions, rtp.RegisterRFC8888())
	case RTCP_RFC8888_PION:
		rtpOptions = append(rtpOptions, rtp.RegisterRFC8888Pion())
	case RTCP_TWCC:
		rtpOptions = append(rtpOptions, rtp.RegisterTWCC())
	}
	server.OnNewHandler(func(h *quic.Handler) {
		// setup media pipeline
		ms, err := media.NewGstreamerSink(sink, mediaOptions...)
		if err != nil {
			panic("TODO") // TODO
		}
		// build interceptor
		i, err := rtp.New(rtpOptions...)
		if err != nil {
			panic("TODO") // TODO
		}
		reader := i.BindRemoteStream(&interceptor.StreamInfo{
			RTPHeaderExtensions: []interceptor.RTPHeaderExtension{{URI: transportCCURI, ID: 1}},
			RTCPFeedback:        []interceptor.RTCPFeedback{{Type: "ack", Parameter: "ccfb"}},
		}, interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
			n, err := ms.Write(b)
			if err != nil {
				return 0, nil, err
			}

			return n, a, nil
		}))

		h.SetRTPReader(interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
			// TODO: Demultiplex flow ID or otherwise use attributes?
			return reader.Read(b, a)
		}))

		i.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
			return h.WriteRTCP(pkts, attributes)
		}))
		go ms.Play()
	})
	return server.Start(ctx)
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
		controller.SetAddr(addr),
		controller.SetRTPLogFileName(rtpDumpFile),
		controller.SetRTCPLogFileName(rtcpDumpFile),
		controller.SetQLOGDirName(qlogDir),
		controller.SetSSLKeyLogFileName(keyLogFile),
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
		return controller.NewQUICServer(mf, controller.DGRAM, options...)
	case "quic-stream":
		options = append(options, controller.MTU(1_000_000))
		return controller.NewQUICServer(mf, controller.STREAM, options...)
	case "quic-prio":
		return controller.NewQUICServer(mf, controller.PRIORITIZED, options...)
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
