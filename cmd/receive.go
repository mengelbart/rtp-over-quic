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

func init() {
	go gstsink.StartMainLoop()
	rootCmd.AddCommand(receiveCmd)
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
	server, err := rtc.NewServer(rtc.CreateGstreamerReceiver, addr)
	if err != nil {
		return err
	}
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
