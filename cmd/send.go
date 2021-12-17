package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lucas-clemente/quic-go"
	gstsrc "github.com/mengelbart/gst-go/gstreamer-src"
	"github.com/mengelbart/rtp-over-quic/rtc"
	"github.com/mengelbart/syncodec"
	"github.com/spf13/cobra"
)

var (
	sendAddr       string
	senderRTPDump  string
	senderRTCPDump string
	senderCodec    string
	source         string
	dumpSent       bool
	scream         bool
	gcc            bool
	sendStream     bool
)

func init() {
	go gstsrc.StartMainLoop()

	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVarP(&sendAddr, "addr", "a", ":4242", "QUIC server address")
	sendCmd.Flags().StringVarP(&senderCodec, "codec", "c", "h264", "Media codec")
	sendCmd.Flags().StringVar(&source, "source", "videotestsrc", "Media source")
	sendCmd.Flags().BoolVarP(&dumpSent, "dump", "d", false, "Dump RTP and RTCP packets to stdout")
	sendCmd.Flags().StringVar(&senderRTPDump, "rtp-dump", "log/rtp_out.log", "RTP dump file")
	sendCmd.Flags().StringVar(&senderRTCPDump, "rtcp-dump", "log/rtcp_out.log", "RTCP dump file")
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
		Dump:     dumpSent,
		RTPDump:  io.Discard,
		RTCPDump: io.Discard,
		SCReAM:   scream,
		GCC:      gcc,
	}
	if dumpSent {
		rtpDumpFile, rtcpDumpFile, err := getDumpFiles(senderRTPDump, senderRTCPDump)
		if err != nil {
			return err
		}
		c.RTPDump = rtpDumpFile
		c.RTCPDump = rtcpDumpFile
		defer rtpDumpFile.Close()
		defer rtcpDumpFile.Close()
	}

	session, err := connectQUIC(sendAddr)
	if err != nil {
		return err
	}

	senderFactory, err := rtc.GstreamerSenderFactory(ctx, c, session)
	if err != nil {
		return err
	}
	src, err := gstSrcPipeline(senderCodec, source, 0, 100_000)
	if err != nil {
		return err
	}
	defer src.Close()
	s, err := senderFactory(src)
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

func gstSrcPipeline(codec string, src string, ssrc uint, initialBitrate uint) (*gstsrc.Pipeline, error) {
	if src != "videotestsrc" {
		src = fmt.Sprintf("filesrc location=%v ! queue ! decodebin ! videoconvert ", src)
	}
	srcPipeline, err := gstsrc.NewPipeline(codec, src)
	if err != nil {
		return nil, err
	}
	srcPipeline.SetSSRC(ssrc)
	srcPipeline.SetBitRate(initialBitrate)
	go srcPipeline.Start()
	return srcPipeline, nil
}

type syntheticEncoder struct {
	io.Reader
	syncodec.Codec
	writer io.Writer
}

func (e *syntheticEncoder) SetBitRate(target uint) {
	e.SetTargetBitrate(int(target))
}

func (e *syntheticEncoder) WriteFrame(frame syncodec.Frame) {
	e.writer.Write(frame.Content)
}

func (e *syntheticEncoder) Close() error {
	return e.Codec.Close()
}

func syncodecPipeline(initialBitrate uint) (rtc.MediaSource, error) {
	reader, writer := io.Pipe()
	sw := &syntheticEncoder{
		Reader: reader,
		Codec:  nil,
		writer: writer,
	}
	encoder, err := syncodec.NewStatisticalEncoder(sw, syncodec.WithInitialTargetBitrate(int(initialBitrate)))
	if err != nil {
		return nil, err
	}
	sw.Codec = encoder
	return sw, nil
}
