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
	receiveAddr      string
	receiverRTPDump  string
	receiverRTCPDump string
	receiverCodec    string
	sink             string
	dumpReceived     bool
	rfc8888          bool
	twcc             bool
)

func init() {
	go gstsink.StartMainLoop()

	rootCmd.AddCommand(receiveCmd)

	receiveCmd.Flags().StringVarP(&receiveAddr, "addr", "a", ":4242", "QUIC server address")
	receiveCmd.Flags().StringVarP(&receiverCodec, "codec", "c", "h264", "Media codec")
	receiveCmd.Flags().StringVar(&sink, "sink", "autovideosink", "Media sink")
	receiveCmd.Flags().BoolVarP(&dumpReceived, "dump", "d", false, "Dump RTP and RTCP packets to stdout")
	receiveCmd.Flags().StringVar(&receiverRTPDump, "rtp-dump", "log/rtp_in.log", "RTP dump file")
	receiveCmd.Flags().StringVar(&receiverRTCPDump, "rtcp-dump", "log/rtcp_in.log", "RTCP dump file")
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

func getDumpFiles(rtpFile, rtcpFile string) (rtpDumpFile io.WriteCloser, rtcpDumpFile io.WriteCloser, err error) {
	rtpDumpFile, err = os.Create(rtpFile)
	if err != nil {
		return
	}
	rtcpDumpFile, err = os.Create(rtcpFile)
	if err != nil {
		return
	}
	return
}

func startReceiver() error {
	c := rtc.ReceiverConfig{
		Dump:     dumpReceived,
		RTPDump:  io.Discard,
		RTCPDump: io.Discard,
		RFC8888:  rfc8888,
		TWCC:     twcc,
	}
	if dumpReceived {
		rtpDumpFile, rtcpDumpfile, err := getDumpFiles(receiverRTPDump, receiverRTCPDump)
		if err != nil {
			return err
		}
		c.RTPDump = rtpDumpFile
		c.RTCPDump = rtcpDumpfile
		defer rtpDumpFile.Close()
		defer rtcpDumpfile.Close()
	}
	receiverFactory, err := rtc.GstreamerReceiverFactory(c)
	if err != nil {
		return err
	}
	server, err := rtc.NewServer(receiverFactory, receiveAddr, gstSinkFactory(receiverCodec, sink))
	if err != nil {
		return err
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error)
	go func() {
		errCh <- server.Listen(ctx)
	}()

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
		dst = fmt.Sprintf("matroskamux ! filesink location=%v", dst)
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
