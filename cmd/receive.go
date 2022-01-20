package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	"github.com/mengelbart/rtp-over-quic/rtc"
	"github.com/spf13/cobra"
)

var (
	receiveTransport string
	receiveAddr      string
	receiverRTPDump  string
	receiverRTCPDump string
	receiverCodec    string
	receiverQLOGDir  string
	sink             string
	rfc8888          bool
	twcc             bool
)

func init() {
	go gstsink.StartMainLoop()

	rootCmd.AddCommand(receiveCmd)

	receiveCmd.Flags().StringVar(&receiveTransport, "transport", "quic", "Transport protocol to use")
	receiveCmd.Flags().StringVarP(&receiveAddr, "addr", "a", ":4242", "QUIC server address")
	receiveCmd.Flags().StringVarP(&receiverCodec, "codec", "c", "h264", "Media codec")
	receiveCmd.Flags().StringVar(&sink, "sink", "autovideosink", "Media sink")
	receiveCmd.Flags().StringVar(&receiverRTPDump, "rtp-dump", "", "RTP dump file")
	receiveCmd.Flags().StringVar(&receiverRTCPDump, "rtcp-dump", "", "RTCP dump file")
	receiveCmd.Flags().StringVar(&receiverQLOGDir, "qlog", "", "QLOG directory. No logs if empty. Use 'sdtout' for Stdout or '<directory>' for a QLOG file named '<directory>/<connection-id>.qlog'")
	receiveCmd.Flags().BoolVarP(&rfc8888, "rfc8888", "r", false, "Send RTCP Feedback for congestion control (RFC 8888)")
	receiveCmd.Flags().BoolVarP(&twcc, "twcc", "t", false, "Send RTCP transport wide congestion control feedback")
}

var receiveCmd = &cobra.Command{
	Use: "receive",
	Run: func(_ *cobra.Command, _ []string) {
		if err := startReceiver(); err != nil {
			log.Fatal(err)
		}
	},
}

func startReceiver() error {
	rtpDumpFile, err := getLogFile(receiverRTPDump)
	if err != nil {
		return err
	}
	defer rtpDumpFile.Close()

	rtcpDumpfile, err := getLogFile(receiverRTCPDump)
	if err != nil {
		return err
	}
	defer rtcpDumpfile.Close()

	c := rtc.ReceiverConfig{
		RTPDump:  rtpDumpFile,
		RTCPDump: rtcpDumpfile,
		RFC8888:  rfc8888,
		TWCC:     twcc,
	}

	receiverFactory, err := rtc.GstreamerReceiverFactory(c)
	if err != nil {
		return err
	}
	tracer, err := getQLOGTracer(receiverQLOGDir)
	if err != nil {
		return err
	}

	var mediaSink rtc.MediaSinkFactory = func() (rtc.MediaSink, error) {
		return nopCloser{io.Discard}, nil
	}
	if receiverCodec != "syncodec" {
		mediaSink = gstSinkFactory(receiverCodec, sink)
	}

	errCh := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch receiveTransport {
	case "quic":
		server, err := rtc.NewServer(receiverFactory, receiveAddr, mediaSink, tracer)
		if err != nil {
			return err
		}
		defer server.Close()

		go func() {
			errCh <- server.Listen(ctx)
		}()

	case "udp":
		server, err := rtc.NewUDPServer(receiverFactory, receiveAddr, mediaSink)
		if err != nil {
			return err
		}

		defer server.Close()

		go func() {
			errCh <- server.Listen(ctx)
		}()
	default:
		return fmt.Errorf("unknown transport protocol: %v", receiveTransport)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigs:
		return nil
	}
}

func gstSinkFactory(codec string, dst string) rtc.MediaSinkFactory {
	if dst != "autovideosink" {
		dst = fmt.Sprintf("clocksync ! y4menc ! filesink location=%v", dst)
	}
	return func() (rtc.MediaSink, error) {
		dstPipeline, err := gstsink.NewPipeline(codec, dst)
		if err != nil {
			return nil, err
		}
		dstPipeline.Start()
		return dstPipeline, nil
	}
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

func discardingSinkFactory() rtc.MediaSinkFactory {
	return func() (rtc.MediaSink, error) {
		return nopCloser{io.Discard}, nil
	}
}

func getLogFile(file string) (io.WriteCloser, error) {
	if len(file) == 0 {
		return nopCloser{io.Discard}, nil
	}
	if file == "stdout" {
		return nopCloser{os.Stdout}, nil
	}
	fd, err := os.Create(file)
	if err != nil {
		return nil, err
	}
	return fd, nil
}
