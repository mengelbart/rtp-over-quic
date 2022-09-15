package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/mengelbart/rtp-over-quic/media"
	"github.com/mengelbart/rtp-over-quic/quic"
	"github.com/mengelbart/rtp-over-quic/rtp"
	"github.com/mengelbart/rtp-over-quic/tcp"
	"github.com/mengelbart/rtp-over-quic/udp"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/spf13/cobra"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type RTCPFeedback int

type handler interface {
	WriteRTCP(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error)
	SetRTPReader(r interceptor.RTPReader)
}

const (
	RTCP_NONE RTCPFeedback = iota
	RTCP_RFC8888
	RTCP_RFC8888_PION
	RTCP_TWCC
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

var receiveCmd = &cobra.Command{
	Use: "receive",
	Run: func(cmd *cobra.Command, _ []string) {
		if err := start(cmd.Context()); err != nil {
			log.Fatal(err)
		}
	},
}

func start(ctx context.Context) error {
	rc := newReceiverController()

	switch transport {
	case "quic", "quic-dgram", "quic-stream", "quic-prio":
		return startQUIC(ctx, rc)
	case "udp":
		return startUDP(ctx, rc)
	case "tcp":
		return startTCP(ctx, rc)
	}
	return fmt.Errorf("%w: %v", errInvalidTransport, transport)
}

func startTCP(ctx context.Context, rc *receiverController) error {
	server, err := tcp.NewServer(
		tcp.LocalAddress(addr),
	)
	if err != nil {
		return err
	}
	server.OnNewHandler(func(h *tcp.Handler) {
		rc.handle(h)
	})
	return server.Start(ctx)
}

func startQUIC(ctx context.Context, rc *receiverController) error {
	server, err := quic.NewServer(
		quic.LocalAddress(addr),
		quic.SetServerQLOGDirName(qlogDir),
		quic.SetServerSSLKeyLogFileName(keyLogFile),
	)
	if err != nil {
		return err
	}
	server.OnNewHandler(func(h *quic.Handler) {
		rc.handle(h)
	})
	return server.Start(ctx)
}

func startUDP(ctx context.Context, rc *receiverController) error {
	server, err := udp.NewServer()
	if err != nil {
		return err
	}
	server.OnNewHandler(func(h *udp.Handler) {
		rc.handle(h)
	})
	return server.Start(ctx)
}

type receiverController struct {
	mediaOptions []media.ConfigOption
	rtpOptions   []rtp.Option
}

func newReceiverController() *receiverController {
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
	return &receiverController{
		mediaOptions: mediaOptions,
		rtpOptions:   rtpOptions,
	}
}

func (c *receiverController) handle(h handler) {
	reader := c.addStream(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
		return h.WriteRTCP(pkts, attributes)
	}))

	h.SetRTPReader(interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		// TODO: Demultiplex flow ID or otherwise use attributes?
		return reader.Read(b, a)
	}))
}

func (c *receiverController) addStream(rtcpWriter interceptor.RTCPWriter) interceptor.RTPReader {
	// setup media pipeline
	ms, err := media.NewGstreamerSink(sink, c.mediaOptions...)
	if err != nil {
		panic("TODO") // TODO
	}
	// build interceptor
	i, err := rtp.New(c.rtpOptions...)
	if err != nil {
		panic("TODO") // TODO
	}

	go func() {
		if err := ms.Play(); err != nil {
			log.Printf("media sink failed to play: %v", err)
		}
	}()

	i.BindRTCPWriter(rtcpWriter)

	return i.BindRemoteStream(&interceptor.StreamInfo{
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{{URI: transportCCURI, ID: 1}},
		RTCPFeedback:        []interceptor.RTCPFeedback{{Type: "ack", Parameter: "ccfb"}},
	}, interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, err := ms.Write(b)
		if err != nil {
			return 0, nil, err
		}

		return n, a, nil
	}))
}

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
