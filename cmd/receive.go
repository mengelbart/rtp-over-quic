package cmd

import (
	"context"
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
	dumpReceived     bool
	rfc8888          bool
	twcc             bool
)

func init() {
	go gstsink.StartMainLoop()

	rootCmd.AddCommand(receiveCmd)

	receiveCmd.Flags().StringVarP(&receiveAddr, "addr", "a", "localhost:4242", "QUIC server address")
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
	server, err := rtc.NewServer(receiverFactory, receiveAddr)
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
