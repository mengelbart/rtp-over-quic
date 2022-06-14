package cmd

import (
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
	mediaFactory := GstreamerSourceFactory(source, mediaOptions...)
	if source == "syncodec" {
		mediaFactory = SyncodecSourceFactory(mediaOptions...)
	}
	options := []controller.Option{
		controller.SetAddr(addr),
		controller.SetRTPLogFileName(rtpDumpFile),
		controller.SetRTCPLogFileName(rtcpDumpFile),
		controller.SetCCLogFileName(ccDump),
		controller.SetQLOGDirName(qlogDir),
		controller.SetSSLKeyLogFileName(keyLogFile),
		controller.SetQUICCongestionControlAlgorithm(controller.CongestionControlAlgorithmFromString(quicCC)),
		controller.SetTCPCongestionControlAlgorithm(controller.CongestionControlAlgorithmFromString(tcpCongAlg)),
		controller.SetRTPCongestionControlAlgorithm(controller.CongestionControlAlgorithmFromString(rtpCC)),
	}
	if sendStream {
		options = append(options, controller.EnableStream())
	}
	if localRFC8888 {
		options = append(options, controller.EnableLocalRFC8888())
	}
	switch transport {
	case "quic", "quic-dgram":
		return runDgramSender(options, mediaFactory)
	case "quic-stream":
	case "udp":
		return runUDPSender(options, mediaFactory)
	case "tcp":
		return runTCPSender(options, mediaFactory)
	}
	return nil
}

func runDgramSender(options []controller.Option, mf controller.MediaSourceFactory) error {
	c, err := controller.NewQUICDgramSender(mf, options...)
	if err != nil {
		return err
	}
	defer c.Close()
	errCh := make(chan error)
	go func() {
		if err := c.Start(); err != nil {
			errCh <- err
		}
	}()
	return waitUntilSignalOrDone(errCh)
}

func runUDPSender(options []controller.Option, mf controller.MediaSourceFactory) error {
	c, err := controller.NewUDPSender(mf, options...)
	if err != nil {
		return err
	}
	defer c.Close()
	errCh := make(chan error)
	go func() {
		if err := c.Start(); err != nil {
			errCh <- err
		}
	}()
	return waitUntilSignalOrDone(errCh)
}

func runTCPSender(options []controller.Option, mf controller.MediaSourceFactory) error {
	c, err := controller.NewTCPSender(mf, options...)
	if err != nil {
		return err
	}
	defer c.Close()
	errCh := make(chan error)
	go func() {
		if err := c.Start(); err != nil {
			errCh <- err
		}
	}()
	return waitUntilSignalOrDone(errCh)
}

func waitUntilSignalOrDone(errCh chan error) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-sigs:
		return nil
	}
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
