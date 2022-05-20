package cmd

import (
	"context"
	"crypto/tls"
	"encoding/binary"
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
	"github.com/mengelbart/rtp-over-quic/rtc/scream"
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
	tcpCongAlg     string
	rtpCC          string
	screamPacer    int
	quicCC         string
	sendStream     bool
	localRFC8888   bool
	keyLogFile     string
)

func init() {
	go gstsrc.StartMainLoop()

	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringVar(&sendTransport, "transport", "quic", "Transport protocol to use: quic, udp or tcp")
	sendCmd.Flags().StringVarP(&sendAddr, "addr", "a", ":4242", "QUIC server address")
	sendCmd.Flags().StringVarP(&senderCodec, "codec", "c", "h264", "Media codec")
	sendCmd.Flags().StringVar(&source, "source", "videotestsrc", "Media source")
	sendCmd.Flags().StringVar(&senderRTPDump, "rtp-dump", "", "RTP dump file, 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&senderRTCPDump, "rtcp-dump", "", "RTCP dump file, 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&ccDump, "cc-dump", "", "Congestion Control log file, use 'stdout' for Stdout")
	sendCmd.Flags().StringVar(&senderQLOGDir, "qlog", "", "QLOG directory. No logs if empty. Use 'sdtout' for Stdout or '<directory>' for a QLOG file named '<directory>/<connection-id>.qlog'")
	sendCmd.Flags().StringVar(&tcpCongAlg, "tcp-congestion", "reno", "TCP Congestion control algorithm to use, only when --transport is tcp")
	sendCmd.Flags().StringVar(&rtpCC, "rtp-cc", "none", "RTP congestion control algorithm. ('none', 'scream', 'gcc')")
	sendCmd.Flags().IntVar(&screamPacer, "scream-pacer", 0, "SCReAM pacer: 0: active wait, 1: active wait and ignore pacing, 2: use pacing timer")
	sendCmd.Flags().BoolVar(&localRFC8888, "local-rfc8888", false, "Generate local RFC 8888 feedback")
	sendCmd.Flags().StringVar(&quicCC, "quic-cc", "none", "QUIC congestion control algorithm. ('none', 'newreno')")
	sendCmd.Flags().BoolVar(&sendStream, "stream", false, "Send random data on a stream")
	sendCmd.Flags().StringVar(&keyLogFile, "keylogfile", "", "TLS keys for decrypting traffic e.g. using wireshark")
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
		RTPDump:      rtpDumpFile,
		RTCPDump:     rtcpDumpFile,
		CCDump:       ccDumpFile,
		CC:           getCC(rtpCC),
		LocalRFC8888: localRFC8888,
		SCReAMPacer:  scream.PacerLoopAlgorithm(screamPacer),
	}

	var transport rtc.Transport
	switch sendTransport {
	case "quic":
		var qlogWriter logging.Tracer
		qlogWriter, err = getQLOGTracer(senderQLOGDir)
		if err != nil {
			return err
		}
		var keyLogger io.Writer
		if len(keyLogFile) > 0 {
			keyLogger, err = os.OpenFile(keyLogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
		}

		var session quic.Connection
		var tracer *rtc.RTTTracer
		session, tracer, err = connectQUIC(qlogWriter, keyLogger)
		if err != nil {
			return err
		}
		transport = &rtc.QUICTransport{
			RTTTracer:  tracer,
			Connection: session,
		}
		if sendStream {
			go streamSendLoop(session)
		}

	case "udp":
		transport, err = connectUDP()
		if err != nil {
			return err
		}

	case "tcp":
		transport, err = connectTCP()
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

func getCC(choice string) rtc.RTPCongestionControlAlgo {
	switch choice {
	case "none":
		return rtc.RTP_CC_NONE
	case "scream":
		return rtc.RTP_CC_SCREAM
	case "gcc":
		return rtc.RTP_CC_GCC
	default:
		log.Printf("WARNING: unknown RTP congestion control algorithm: %v, using default ('none')\n", choice)
		return rtc.RTP_CC_NONE
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

func connectQUIC(qlogger logging.Tracer, keyLogger io.Writer) (quic.Connection, *rtc.RTTTracer, error) {
	tlsConf := &tls.Config{
		KeyLogWriter:       keyLogger,
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	metricsTracer := rtc.NewTracer()
	tracers := []logging.Tracer{metricsTracer}
	if qlogger != nil {
		tracers = append(tracers, qlogger)
	}
	tracer := logging.NewMultiplexedTracer(tracers...)
	quicConf := &quic.Config{
		EnableDatagrams:      true,
		HandshakeIdleTimeout: 15 * time.Second,
		Tracer:               tracer,
		DisableCC:            quicCC != "newreno",
	}
	session, err := quic.DialAddr(sendAddr, tlsConf, quicConf)
	if err != nil {
		return nil, nil, err
	}
	return session, metricsTracer, nil
}

func streamSendLoop(session quic.Connection) error {
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
		src = fmt.Sprintf("filesrc location=%v ! decodebin ! clocksync ", src)
	} else {
		src = "videotestsrc ! clocksync "
	}
	srcPipeline, err := gstsrc.NewPipeline(codec, src)
	if err != nil {
		return nil, err
	}
	log.Printf("run gstreamer pipeline: [%v]", srcPipeline.String())
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

func (t *udpClient) Metrics() rtc.RTTStats {
	panic(fmt.Errorf("UDP does not provide metrics"))
}

func connectTCP() (*tcpClient, error) {
	dialer := &net.Dialer{
		Control: func(_, _ string, c syscall.RawConn) error {
			var operr error
			if err := c.Control(func(fd uintptr) {
				operr = syscall.SetsockoptString(int(fd), syscall.IPPROTO_TCP, syscall.TCP_CONGESTION, tcpCongAlg)
			}); err != nil {
				return err
			}
			if operr != nil {
				return fmt.Errorf("failed to set TCP congestion control algorithm to '%v': %w", tcpCongAlg, operr)
			}
			return nil
		},
	}
	conn, err := dialer.Dial("tcp", sendAddr)
	if err != nil {
		return nil, err
	}
	// TODO: Use something like this for tcp cwnd logging:
	//go func() {
	//	t := time.NewTicker(200 * time.Millisecond)
	//	for range t.C {
	//		conn, ok := conn.(interface {
	//			SyscallConn() (syscall.RawConn, error)
	//		})
	//		if !ok {
	//			panic(errors.New("doesn't have a SyscallConn"))
	//		}
	//		rawConn, err := conn.SyscallConn()
	//		if err != nil {
	//			panic(fmt.Errorf("couldn't get syscall.RawConn: %w", err))
	//		}
	//		rawConn.Control(func(fd uintptr) {
	//			info, serr := unix.GetsockoptTCPInfo(int(fd), unix.SOL_TCP, unix.TCP_INFO)
	//			if serr != nil {
	//				panic(serr)
	//			}
	//			fmt.Printf("%v\n", info.Snd_cwnd)
	//		})
	//	}
	//}()
	return &tcpClient{
		conn: conn,
	}, nil
}

type tcpClient struct {
	conn net.Conn
}

func (c *tcpClient) SendMessage(msg []byte, _ func(error), _ func(bool)) error {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(msg)))
	n, err := c.conn.Write(append(buf, msg...))
	if err != nil {
		return err
	}
	if n != len(msg)+2 {
		log.Fatalf("not enough bytes written to tcp conn: n=%v, expected=%v\n", n, len(msg)+2)
	}
	return nil
}

func (c *tcpClient) ReceiveMessage() ([]byte, error) {
	prefix := make([]byte, 2)
	if _, err := io.ReadFull(c.conn, prefix); err != nil {
		return nil, fmt.Errorf("failed to read length from TCP conn: %w, exiting", err)
	}
	length := binary.BigEndian.Uint16(prefix)
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.conn, buf); err != nil {

		return nil, fmt.Errorf("failed to read complete frame from TCP conn: %w, exiting", err)
	}
	return buf, nil
}

func (c *tcpClient) CloseWithError(int, string) error {
	return c.conn.Close()
}

func (t *tcpClient) Metrics() rtc.RTTStats {
	panic(fmt.Errorf("TCP does not provide metrics"))
}
