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
	srcPipeline, err := gstsrc.NewPipeline("vp8", "videotestsrc")
	if err != nil {
		return err
	}
	s, err := rtc.NewSender(srcPipeline, addr)
	if err != nil {
		return err
	}
	go srcPipeline.Start()
	defer func() {
		srcPipeline.Stop()
		srcPipeline.Destroy()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error)
	go func() {
		errCh <- s.Run(ctx)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigs:
		fmt.Println("GOT SIGNAL")
		return nil
	}
}
