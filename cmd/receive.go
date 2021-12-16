package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	"github.com/mengelbart/rtp-over-quic/rtc"
	"github.com/spf13/cobra"
)

var (
	dumpReceived, rfc8888, twcc bool
)

func init() {
	go gstsink.StartMainLoop()

	rootCmd.AddCommand(receiveCmd)

	receiveCmd.Flags().BoolVarP(&dumpReceived, "dump", "d", false, "Dump RTP and RTCP packets to stdout")
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
	c := rtc.ReceiverConfig{
		Dump:    dumpReceived,
		RFC8888: rfc8888,
		TWCC:    twcc,
	}
	server, err := rtc.NewServer(rtc.GstreamerReceiverFactory(c), addr)
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
