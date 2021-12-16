package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
	"github.com/mengelbart/rtp-over-quic/rtc"
	"github.com/spf13/cobra"
)

func init() {
	go gstsrc.StartMainLoop()
	rootCmd.AddCommand(sendCmd)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := rtc.SenderConfig{
		Dump:   false,
		SCReAM: false,
		GCC:    false,
	}
	s, err := rtc.GstreamerSenderFactory(ctx, c)(addr)
	if err != nil {
		return err
	}

	defer s.Close()
	errCh := make(chan error)
	go func() {
		errCh <- s.Run()
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		fmt.Println("Got error")
		return err
	case <-sigs:
		return nil
	}
}
