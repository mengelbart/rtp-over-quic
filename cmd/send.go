package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lucas-clemente/quic-go"
	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
	"github.com/mengelbart/rtp-over-quic/rtc"
	"github.com/spf13/cobra"
)

var (
	sendAddr string
	dumpSent,
	scream,
	gcc,
	sendStream bool
)

func init() {
	go gstsrc.StartMainLoop()

	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVarP(&sendAddr, "addr", "a", "localhost:4242", "QUIC server address")
	sendCmd.Flags().BoolVarP(&dumpSent, "dump", "d", false, "Dump RTP and RTCP packets to stdout")
	sendCmd.Flags().BoolVarP(&scream, "scream", "s", false, "Use SCReAM")
	sendCmd.Flags().BoolVarP(&gcc, "gcc", "g", false, "Use Google Congestion Control")
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := rtc.SenderConfig{
		Dump:   dumpSent,
		SCReAM: scream,
		GCC:    gcc,
	}

	session, err := connectQUIC(sendAddr)
	if err != nil {
		return err
	}

	s, err := rtc.GstreamerSenderFactory(ctx, c, session)()
	if err != nil {
		return err
	}

	defer s.Close()
	errCh := make(chan error)
	go func() {
		errCh <- s.Run()
	}()

	if sendStream {
		go streamSendLoop(session)
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

func connectQUIC(addr string) (quic.Session, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	quicConf := &quic.Config{
		MaxIdleTimeout:  time.Second,
		EnableDatagrams: true,
	}
	session, err := quic.DialAddr(addr, tlsConf, quicConf)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func streamSendLoop(session quic.Session) error {
	fmt.Println("Open stream")
	stream, err := session.OpenUniStream()
	if err != nil {
		return err
	}
	fmt.Println("Opened stream")
	buf := make([]byte, 1200)
	for {
		_, err := stream.Write(buf)
		if err != nil {
			return err
		}
	}
}
