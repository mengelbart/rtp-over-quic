package controller

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/mengelbart/rtp-over-quic/rtc/scream"
	"github.com/mengelbart/rtp-over-quic/transport"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
)

type baseSender struct {
	// config
	addr              string
	quicCC            CongestionControlAlgorithm
	qlogDirName       string
	sslKeyLogFileName string

	tcpCC CongestionControlAlgorithm

	rtpCC        CongestionControlAlgorithm
	localRFC8888 bool

	stream bool

	rtpLogFileName  string
	rtcpLogFileName string
	ccLogFileName   string

	// implementation
	registry  *interceptor.Registry
	ccLogFile io.WriteCloser

	// TODO: Bundle flow and media and store as list to allow multiple streams
	flow         *transport.Flow
	mediaFactory MediaSourceFactory
	media        MediaSource

	bweChan chan scream.BandwidthEstimator

	rtcpChan chan rtcpFeedback

	close chan struct{}
}

func newBaseSender(media MediaSourceFactory, opts ...Option) (*baseSender, error) {
	c := &baseSender{
		addr:              ":4242",
		quicCC:            0,
		qlogDirName:       "",
		sslKeyLogFileName: "",
		rtpCC:             0,
		localRFC8888:      false,
		stream:            false,
		rtpLogFileName:    "",
		rtcpLogFileName:   "",
		ccLogFileName:     "",
		registry:          &interceptor.Registry{},
		ccLogFile:         nil,
		flow:              transport.NewFlowWithID(0),
		mediaFactory:      media,
		media:             nil,
		bweChan:           make(chan scream.BandwidthEstimator),
		rtcpChan:          make(chan rtcpFeedback),
		close:             make(chan struct{}),
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	if len(c.rtpLogFileName) > 0 {
		if err := c.registerPacketLog(c.registry); err != nil {
			return nil, err
		}
	}
	if c.rtpCC == SCReAM {
		if err := registerSCReAM(c.registry, c.onNewSCrEAMEstimator); err != nil {
			return nil, err
		}
	}
	if c.rtpCC == GCC {
		if err := registerTWCCHeaderExtension(c.registry); err != nil {
			return nil, err
		}
		if err := registerGCC(c.registry, c.OnBandwidthEstimationUpdate); err != nil {
			return nil, err
		}
	}
	ccLogFile, err := getLogFile(c.ccLogFileName)
	if err != nil {
		return nil, err
	}
	c.ccLogFile = ccLogFile
	return c, nil
}

func (s *baseSender) onNewSCrEAMEstimator(_ string, bwe scream.BandwidthEstimator) {
	go func() {
		s.bweChan <- bwe
	}()
}

func (s *baseSender) Start() error {
	i, err := s.registry.Build("")
	if err != nil {
		return err
	}
	go s.runBandwidthEstimation()
	writer := i.BindLocalStream(&interceptor.StreamInfo{
		ID:                  "",
		Attributes:          map[interface{}]interface{}{},
		SSRC:                0,
		PayloadType:         0,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{{URI: transportCCURI, ID: 1}},
		MimeType:            "",
		ClockRate:           0,
		Channels:            0,
		SDPFmtpLine:         "",
		RTCPFeedback: []interceptor.RTCPFeedback{{
			Type:      "ack",
			Parameter: "ccfb",
		}},
	}, s.flow)
	m, err := s.mediaFactory.Create(writer)
	if err != nil {
		return err
	}
	s.media = m
	reader := i.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	errCh := make(chan error)
	go func() {
		if err := s.readRTCP(reader); err != nil {
			log.Printf("failed to read RTCP: %v", err)
			errCh <- err
		}
	}()
	go func() {
		if err := s.media.Play(); err != nil {
			log.Printf("failed to play media: %v", err)
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-s.close:
	}
	return nil
}

func (s *baseSender) readRTCP(reader interceptor.RTCPReader) error {
	for {
		select {
		case report := <-s.rtcpChan:
			_, _, err := reader.Read(report.buf, report.attributes)
			if err != nil {
				log.Printf("RTCP reader returned error: %v", err)
				return err
			}
		case <-s.close:
			return nil
		}
	}
}

func (s *baseSender) OnBandwidthEstimationUpdate(id string, bwe cc.BandwidthEstimator) {
	target := bwe.GetTargetBitrate()
	if target < 0 {
		log.Printf("got negative target bitrate: %v", target)
		return
	}
	s.media.SetTargetBitrate(uint(target))
}

func (s *baseSender) runBandwidthEstimation() {
	ticker := time.NewTicker(100 * time.Millisecond)

	var bwe scream.BandwidthEstimator
	select {
	case bwe = <-s.bweChan:
	case <-s.close:
		return
	}

	for {
		select {
		case bwe = <-s.bweChan:
		case now := <-ticker.C:
			target, err := bwe.GetTargetBitrate(0)
			if err != nil {
				log.Printf("got error on bwe.GetTargetBitrate: %v", err)
				continue
			}
			if target < 0 {
				log.Printf("[SCReAM] got negative target bitrate: %v", target)
				continue
			}
			stats := bwe.GetStats()
			fmt.Fprintf(
				s.ccLogFile, "%v, %v, %v, %v, %v, %v, %v, %v, %v, %v\n",
				now.UnixMilli(),
				target,
				stats["queueDelay"],
				stats["sRTT"],
				stats["cwnd"],
				stats["bytesInFlightLog"],
				stats["rateLostStream0"],
				stats["rateTransmittedStream0"],
				stats["rateAckedStream0"],
				stats["hiSeqAckStream0"],
			)
			s.media.SetTargetBitrate(uint(target))
		case <-s.close:
			return
		}
	}
}

func (s *baseSender) isClosed() bool {
	select {
	case <-s.close:
		return true
	default:
		return false
	}
}

func (s *baseSender) Close() error {
	if !s.isClosed() {
		close(s.close)
	}
	s.media.Stop()
	return nil
}

type Option func(*baseSender) error

func SetTCPCongestionControlAlgorithm(a CongestionControlAlgorithm) Option {
	return func(s *baseSender) error {
		s.tcpCC = a
		return nil
	}
}

func SetQUICCongestionControlAlgorithm(a CongestionControlAlgorithm) Option {
	return func(s *baseSender) error {
		s.quicCC = a
		return nil
	}
}

func SetCCLogFileName(name string) Option {
	return func(s *baseSender) error {
		s.ccLogFileName = name
		return nil
	}
}

func SetRTCPLogFileName(name string) Option {
	return func(s *baseSender) error {
		s.rtcpLogFileName = name
		return nil
	}
}

func SetRTPLogFileName(name string) Option {
	return func(s *baseSender) error {
		s.rtpLogFileName = name
		return nil
	}
}

func EnableStream() Option {
	return func(s *baseSender) error {
		s.stream = true
		return nil
	}
}

func DisableStream() Option {
	return func(s *baseSender) error {
		s.stream = false
		return nil
	}
}

func EnableLocalRFC8888() Option {
	return func(s *baseSender) error {
		s.localRFC8888 = true
		return nil
	}
}

func DisableLocalRFC8888() Option {
	return func(s *baseSender) error {
		s.localRFC8888 = false
		return nil
	}
}

func SetRTPCongestionControlAlgorithm(algorithm CongestionControlAlgorithm) Option {
	return func(s *baseSender) error {
		s.rtpCC = algorithm
		return nil
	}
}

func SetSSLKeyLogFileName(name string) Option {
	return func(s *baseSender) error {
		s.sslKeyLogFileName = name
		return nil
	}
}

func SetQLOGDirName(name string) Option {
	return func(s *baseSender) error {
		s.qlogDirName = name
		return nil
	}
}

func SetStream(b bool) Option {
	return func(s *baseSender) error {
		s.stream = true
		return nil
	}
}

func SetAddr(addr string) Option {
	return func(s *baseSender) error {
		s.addr = addr
		return nil
	}
}

func (c *baseSender) registerPacketLog(registry *interceptor.Registry) error {
	rtpDumpFile, err := getLogFile(c.rtpLogFileName)
	if err != nil {
		return err
	}
	rtcpDumpFile, err := getLogFile(c.rtcpLogFileName)
	if err != nil {
		return err
	}
	return registerRTPSenderDumper(registry, rtpDumpFile, rtcpDumpFile)
}
