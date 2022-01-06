package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
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
	sendTransport  string
	sendAddr       string
	senderRTPDump  string
	senderRTCPDump string
	senderCodec    string
	source         string
	ccDump         string
	senderQLOGDir  string
	scream         bool
	gcc            bool
	newReno        bool
	sendStream     bool
)

func init() {
	go gstsrc.StartMainLoop()

	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVar(&sendTransport, "transport", "quic", "Transport protocol to use: quic or udp")
	sendCmd.Flags().StringVarP(&sendAddr, "addr", "a", ":4242", "QUIC server address")
	sendCmd.Flags().StringVarP(&senderCodec, "codec", "c", "h264", "Media codec")
	sendCmd.Flags().StringVar(&source, "source", "videotestsrc", "Media source")
	sendCmd.Flags().StringVar(&senderRTPDump, "rtp-dump", "", "RTP dump file, 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&senderRTCPDump, "rtcp-dump", "", "RTCP dump file, 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&ccDump, "cc-dump", "", "Congestion Control log file, use 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&senderQLOGDir, "qlog", "", "QLOG directory. No logs if empty. Use 'sdtout' for Stdout or '<directory>' for a QLOG file named '<directory>/<connection-id>.qlog'")
	sendCmd.Flags().BoolVarP(&scream, "scream", "s", false, "Use SCReAM")
	sendCmd.Flags().BoolVarP(&gcc, "gcc", "g", false, "Use Google Congestion Control")
	sendCmd.Flags().BoolVarP(&newReno, "newreno", "n", false, "Enable NewReno Congestion Control")
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
	fmt.Printf("newReno: %v\n", newReno)
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

	var transport rtc.Transport
	switch sendTransport {
	case "quic":
		var qlogWriter logging.Tracer
		qlogWriter, err = getQLOGTracer(senderQLOGDir)
		if err != nil {
			return err
		}
		var session quic.Session
		session, err = connectQUIC(qlogWriter)
		if err != nil {
			return err
		}
		transport = &rtc.QUICTransport{
			Session: session,
		}
		if sendStream {
			go streamSendLoop(session)
		}

	case "udp":
		transport, err = connectUDP()
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown transport protocol: %v", sendTransport)
	}
	senderFactory, err := rtc.GstreamerSenderFactory(ctx, c, transport)
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

func connectQUIC(tracer logging.Tracer) (quic.Session, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	quicConf := &quic.Config{
		MaxIdleTimeout:  time.Second,
		EnableDatagrams: true,
		Tracer:          tracer,
		DisableCC:       !newReno,
	}
	session, err := quic.DialAddr(sendAddr, tlsConf, quicConf)
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

func connectUDP() (*udpClient, error) {
	a, err := net.ResolveUDPAddr("udp", sendAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, a)
	if err != nil {
		return nil, err
	}
	return &udpClient{
		conn: conn,
	}, nil
}

type udpClient struct {
	conn *net.UDPConn
}

func (c *udpClient) SendMessage(msg []byte, _ func(error), _ func(bool)) error {
	_, err := c.conn.Write(msg)
	return err
}

func (c *udpClient) ReceiveMessage() ([]byte, error) {
	buf := make([]byte, 1400)
	n, err := c.conn.Read(buf)
	return buf[:n], err
}

func (c *udpClient) CloseWithError(int, string) error {
	return c.conn.Close()
}
