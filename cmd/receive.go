package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lucas-clemente/quic-go/logging"
	"github.com/lucas-clemente/quic-go/qlog"
	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	"github.com/mengelbart/rtp-over-quic/rtc"
	"github.com/spf13/cobra"
)

var (
	sink         string
	rtcpFeedback string
)

func init() {
	go gstsink.StartMainLoop()
	rootCmd.AddCommand(receiveCmd)
	receiveCmd.Flags().StringVar(&sink, "sink", "autovideosink", "Media sink")
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
	rtpDumpFile, err := getLogFile(rtpDumpFile)
	if err != nil {
		return err
	}
	defer rtpDumpFile.Close()

	rtcpDumpfile, err := getLogFile(rtcpDumpFile)
	if err != nil {
		return err
	}
	defer rtcpDumpfile.Close()

	c := rtc.ReceiverConfig{
		RTPDump:  rtpDumpFile,
		RTCPDump: rtcpDumpfile,
		Feedback: getRTCP(rtcpFeedback),
	}

	receiverFactory, err := rtc.GstreamerReceiverFactory(c)
	if err != nil {
		return err
	}
	tracer, err := getQLOGTracer(qlogDir)
	if err != nil {
		return err
	}

	var mediaSink rtc.MediaSinkFactory = func() (rtc.MediaSink, error) {
		return nopCloser{io.Discard}, nil
	}
	if codec != "syncodec" {
		mediaSink = gstSinkFactory(codec, sink)
	}

	errCh := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch transport {
	case "quic":
		server, err := rtc.NewServer(receiverFactory, addr, mediaSink, tracer)
		if err != nil {
			return err
		}
		defer server.Close()

		go func() {
			errCh <- server.Listen(ctx)
		}()

	case "udp":
		server, err := rtc.NewUDPServer(receiverFactory, addr, mediaSink)
		if err != nil {
			return err
		}

		defer server.Close()

		go func() {
			errCh <- server.Listen(ctx)
		}()

	case "tcp":
		server, err := rtc.NewTCPServer(receiverFactory, addr, mediaSink)
		if err != nil {
			return err
		}

		defer server.Close()

		go func() {
			errCh <- server.Listen(ctx)
		}()
	default:
		return fmt.Errorf("unknown transport protocol: %v", transport)
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

func getRTCP(choice string) rtc.RTCPFeedback {
	switch choice {
	case "none":
		return rtc.RTCP_NONE
	case "rfc8888":
		return rtc.RTCP_RFC8888
	case "rfc8888-pion":
		return rtc.RTCP_RFC8888_PION
	case "twcc":
		return rtc.RTCP_TWCC
	default:
		log.Printf("WARNING: unknown RTCP Congestion Control Feedback type: %v, using default ('none')\n", choice)
		return rtc.RTCP_NONE
	}
}

func gstSinkFactory(codec string, dst string) rtc.MediaSinkFactory {
	if dst != "autovideosink" {
		dst = fmt.Sprintf("clocksync ! y4menc ! filesink location=%v", dst)
	} else {
		dst = "clocksync ! autovideosink"
	}
	return func() (rtc.MediaSink, error) {
		dstPipeline, err := gstsink.NewPipeline(codec, dst)
		if err != nil {
			return nil, err
		}
		log.Printf("run gstreamer pipeline: [%v]", dstPipeline.String())
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

type fileCloser struct {
	f   *os.File
	buf *bufio.Writer
}

func (f *fileCloser) Write(buf []byte) (int, error) {
	return f.f.Write(buf)
}

func (f *fileCloser) Close() error {
	if err := f.buf.Flush(); err != nil {
		log.Printf("failed to flush: %v\n", err)
	}
	return f.f.Close()
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
	bufwriter := bufio.NewWriterSize(fd, 4096)

	return &fileCloser{
		f:   fd,
		buf: bufwriter,
	}, nil
}

func getQLOGTracer(path string) (logging.Tracer, error) {
	if len(path) == 0 {
		return nil, nil
	}
	if path == "stdout" {
		return qlog.NewTracer(func(p logging.Perspective, connectionID []byte) io.WriteCloser {
			return nopCloser{os.Stdout}
		}), nil
	}
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(path, 0o666); err != nil {
				return nil, fmt.Errorf("failed to create qlog dir %s: %v", path, err)
			}
		} else {
			return nil, err
		}
	}
	return qlog.NewTracer(func(p logging.Perspective, connectionID []byte) io.WriteCloser {
		file := fmt.Sprintf("%s/%x_%v.qlog", strings.TrimRight(path, "/"), connectionID, p)
		w, err := os.Create(file)
		if err != nil {
			log.Printf("failed to create qlog file %s: %v", path, err)
			return nil
		}
		log.Printf("created qlog file: %s\n", path)
		return w
	}), nil
}
