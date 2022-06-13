package controller

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/mengelbart/rtp-over-quic/rtc/scream"
	"github.com/mengelbart/rtp-over-quic/transport"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type MediaSource interface {
	Play() error
	SetTargetBitrate(uint)
}

type MediaSourceFactory interface {
	Create(interceptor.RTPWriter) (MediaSource, error)
}

type rtcpFeedback struct {
	buf        []byte
	attributes interceptor.Attributes
}

type QUICDgramSender struct {
	conn      quic.Connection
	transport *transport.Dgram

	registry *interceptor.Registry

	// TODO: Bundle flow and media and store as list to allow multiple streams
	flow         *transport.Flow
	mediaFactory MediaSourceFactory
	media        MediaSource

	rtcpChan chan rtcpFeedback

	config *CommonConfig

	ccLogFile io.WriteCloser

	close chan struct{}
}

func NewQUICDgramSender(media MediaSourceFactory, opts ...Option) (*QUICDgramSender, error) {
	config := newDefaultConfig()
	for _, opt := range opts {
		if err := opt(config); err != nil {
			return nil, err
		}
	}
	var connection quic.Connection
	var tracer *RTTTracer
	var err error
	if config.localRFC8888 {
		tracer = NewTracer()
		connection, err = connectQUIC(
			config.addr,
			config.quicCC,
			tracer,
			config.qlogDirName,
			config.sslKeyLogFileName,
		)
	} else {
		connection, err = connectQUIC(
			config.addr,
			config.quicCC,
			nil,
			config.qlogDirName,
			config.sslKeyLogFileName,
		)
	}
	if err != nil {
		return nil, err
	}
	s := &QUICDgramSender{
		conn:         connection,
		transport:    transport.NewDgramTransportWithConn(connection),
		registry:     &interceptor.Registry{},
		flow:         &transport.Flow{},
		mediaFactory: media,
		media:        nil,
		rtcpChan:     make(chan rtcpFeedback),
		config:       config,
		ccLogFile:    nil,
		close:        make(chan struct{}),
	}

	if len(s.config.rtpLogFileName) > 0 {
		if err = s.config.registerPacketLog(s.registry); err != nil {
			return nil, err
		}
	}

	if s.config.rtpCC == SCReAM {
		if err = registerSCReAM(s.registry, s.onSCrEAMEstimationUpdate); err != nil {
			return nil, err
		}
	}
	if s.config.rtpCC == GCC {
		if err = registerTWCCHeaderExtension(s.registry); err != nil {
			return nil, err
		}
		if err = registerGCC(s.registry, s.OnBandwidthEstimationUpdate); err != nil {
			return nil, err
		}
	}
	ccLogFile, err := getLogFile(s.config.ccLogFileName)
	if err != nil {
		return nil, err
	}
	s.ccLogFile = ccLogFile

	s.flow = transport.NewFlowWithID(0)
	s.flow.Bind(s.transport)

	if s.config.localRFC8888 {
		s.flow.EnableLocalFeedback(0, tracer, func(f transport.Feedback) {
			s.rtcpChan <- rtcpFeedback{
				buf:        f.Buf,
				attributes: map[interface{}]interface{}{"timestamp": f.Timestamp},
			}
		})
	}

	return s, nil
}

func (s *QUICDgramSender) Start() error {
	i, err := s.registry.Build("")
	if err != nil {
		return err
	}
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
	go func() {
		if err := s.readRTCP(reader); err != nil {
			log.Printf("failed to read RTCP: %v", err)
		}
	}()
	go func() {
		if err := s.readRTCPFromNetwork(); err != nil {
			log.Printf("failed to read from network: %v", err)
		}
	}()
	return s.media.Play()
}

func (s *QUICDgramSender) readRTCP(reader interceptor.RTCPReader) error {
	for report := range s.rtcpChan {
		_, _, err := reader.Read(report.buf, report.attributes)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *QUICDgramSender) readRTCPFromNetwork() error {
	buf := make([]byte, 1500)
	for {
		n, err := s.transport.Read(buf)
		if err != nil {
			return err
		}
		s.rtcpChan <- rtcpFeedback{
			buf:        buf[:n],
			attributes: nil,
		}
	}
}

func (s *QUICDgramSender) OnBandwidthEstimationUpdate(id string, bwe cc.BandwidthEstimator) {
	target := bwe.GetTargetBitrate()
	if target < 0 {
		log.Printf("got negative target bitrate: %v", target)
		return
	}
	s.media.SetTargetBitrate(uint(target))
}

func (s *QUICDgramSender) onSCrEAMEstimationUpdate(id string, bwe scream.BandwidthEstimator) {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for {
			select {
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
	}()
}

func (s *QUICDgramSender) isClosed() bool {
	select {
	case <-s.close:
		return true
	default:
		return false
	}
}

func (s *QUICDgramSender) Close() error {
	if err := s.flow.Close(); err != nil {
		return err
	}
	if !s.isClosed() {
		close(s.close)
	}
	return s.conn.CloseWithError(0, "bye")
}
