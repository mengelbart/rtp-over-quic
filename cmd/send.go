package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/logging"
	"github.com/lucas-clemente/quic-go/qlog"
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
	ccDump         string
	senderQLOGDir  string
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
	sendCmd.Flags().StringVar(&senderRTPDump, "rtp-dump", "", "RTP dump file, 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&senderRTCPDump, "rtcp-dump", "", "RTCP dump file, 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&ccDump, "cc-dump", "", "Congestion Control log file, use 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&senderQLOGDir, "qlog", "", "QLOG directory. No logs if empty. Use 'sdtout' for Stdout or '<directory>' for a QLOG file named '<directory>/<connection-id>.qlog'")
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

	ccDumpFile, err := getLogFile(ccDump)
	if err != nil {
		return err
	}
	defer ccDumpFile.Close()
	rtpDumpFile, err := getLogFile(senderRTPDump)
	if err != nil {
		return err
	}
	rtcpDumpFile, err := getLogFile(senderRTCPDump)
	if err != nil {
		return err
	}
	defer rtpDumpFile.Close()
	defer rtcpDumpFile.Close()

	c := rtc.SenderConfig{
		RTPDump:  rtpDumpFile,
		RTCPDump: rtcpDumpFile,
		CCDump:   ccDumpFile,
		SCReAM:   scream,
		GCC:      gcc,
	}

	qlogWriter, err := getQLOGTracer(senderQLOGDir)
	if err != nil {
		return err
	}
	session, err := connectQUIC(sendAddr, qlogWriter)
	if err != nil {
		return err
	}
	senderFactory, err := rtc.GstreamerSenderFactory(ctx, c, session)
	if err != nil {
		return err
	}

	var src rtc.MediaSource
	if senderCodec == "syncodec" {
		src, err = syncodecPipeline(100_000)
		if err != nil {
			return err
		}
	} else {
		var gstSrc *gstsrc.Pipeline
		gstSrc, err = gstSrcPipeline(senderCodec, source, 0, 100_000)
		if err != nil {
			return err
		}
		defer gstSrc.Close()
		src = gstSrc
	}

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
	return qlog.NewTracer(func(_ logging.Perspective, connectionID []byte) io.WriteCloser {
		file := fmt.Sprintf("%s/%x.qlog", strings.TrimRight(path, "/"), connectionID)
		w, err := os.Create(file)
		if err != nil {
			log.Printf("failed to create qlog file %s: %v", path, err)
			return nil
		}
		log.Printf("created qlog file: %s\n", path)
		return w
	}), nil
}

func connectQUIC(addr string, tracer logging.Tracer) (quic.Session, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	quicConf := &quic.Config{
		MaxIdleTimeout:  time.Second,
		EnableDatagrams: true,
		Tracer:          tracer,
	}
	session, err := quic.DialAddr(addr, tlsConf, quicConf)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func streamSendLoop(session quic.Session) error {
	log.Println("Open stream")
	stream, err := session.OpenUniStream()
	if err != nil {
		return err
	}
	log.Println("Opened stream")
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
	go sw.Start()
	return sw, nil
}
