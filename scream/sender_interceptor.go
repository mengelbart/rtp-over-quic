package scream

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mengelbart/scream-go"
	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type BandwidthEstimator interface {
	GetTargetBitrate(ssrc uint32) (int, error)
	GetStats() map[string]interface{}
}

type NewPeerConnectionCallback func(id string, estimator BandwidthEstimator)

// RTPQueue implements the packet queue which will be used by SCReAM to buffer packets
type RTPQueue interface {
	scream.RTPQueue
	// Enqueue adds a new packet to the end of the queue.
	Enqueue(packet *packet, ts float64)
	// Dequeue removes and returns the first packet in the queue.
	Dequeue() *packet
}

type localStream struct {
	queue       RTPQueue
	newFrame    chan struct{}
	newFeedback chan struct{}
	close       chan struct{}
}

type SenderInterceptorFactory struct {
	opts              []SenderOption
	addPeerConnection NewPeerConnectionCallback
}

func NewSenderInterceptor(opts ...SenderOption) (*SenderInterceptorFactory, error) {
	return &SenderInterceptorFactory{
		opts: opts,
	}, nil
}

func (f *SenderInterceptorFactory) OnNewPeerConnection(cb NewPeerConnectionCallback) {
	f.addPeerConnection = cb
}

func (f *SenderInterceptorFactory) NewInterceptor(id string) (interceptor.Interceptor, error) {
	s := &SenderInterceptor{
		NoOp:           interceptor.NoOp{},
		m:              sync.Mutex{},
		wg:             sync.WaitGroup{},
		tx:             scream.NewTx(),
		close:          make(chan struct{}),
		log:            logging.NewDefaultLoggerFactory().NewLogger("scream_sender"),
		newRTPQueue:    newQueue,
		rtpStreams:     map[uint32]*localStream{},
		rtpStreamsMu:   sync.Mutex{},
		minBitrate:     100_000,
		initialBitrate: 500_000,
		maxBitrate:     100_000_000,
	}
	for _, opt := range f.opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}
	if f.addPeerConnection != nil {
		f.addPeerConnection(id, s)
	}
	return s, nil
}

// SenderInterceptor performs SCReAM congestion control
type SenderInterceptor struct {
	interceptor.NoOp
	m     sync.Mutex
	wg    sync.WaitGroup
	tx    *scream.Tx
	close chan struct{}
	log   logging.LeveledLogger

	newRTPQueue  func() RTPQueue
	rtpStreams   map[uint32]*localStream
	rtpStreamsMu sync.Mutex

	minBitrate     float64
	initialBitrate float64
	maxBitrate     float64
}

func (s *SenderInterceptor) getTimeNTP(t time.Time) uint64 {
	return uint64(ntpTime32(t))
}

// BindRTCPReader lets you modify any incoming RTCP packets. It is called once per sender/receiver, however this might
// change in the future. The returned method will be called once per packet batch.
func (s *SenderInterceptor) BindRTCPReader(reader interceptor.RTCPReader) interceptor.RTCPReader {
	return interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		timestamp := time.Now()
		if ts, ok := a["timestamp"]; ok {
			if t, ok := ts.(time.Time); ok {
				timestamp = t
			}
		}
		t := s.getTimeNTP(timestamp)

		n, attr, err := reader.Read(b, a)
		if err != nil {
			return 0, nil, err
		}
		buf := make([]byte, n)
		copy(buf, b)
		pkts, err := rtcp.Unmarshal(buf)
		if err != nil {
			return 0, nil, err
		}

		for _, pkt := range pkts {

			var ssrcs []uint32
			switch report := pkt.(type) {
			case *rtcp.RawPacket:
				ssrcs = extractSSRCs(*report)
			case *rtcp.CCFeedbackReport:
				ssrcs = extractSSRCsReport(report)
			default:
				s.log.Info("got incorrect packet type, skipping feedback")
				continue
			}

			s.m.Lock()
			s.tx.IncomingStandardizedFeedback(t, b[:n])
			s.m.Unlock()

			for _, ssrc := range ssrcs {
				s.rtpStreamsMu.Lock()
				if stream, ok := s.rtpStreams[ssrc]; ok {
					stream.newFeedback <- struct{}{}
				}
				s.rtpStreamsMu.Unlock()
			}
		}

		return n, attr, nil
	})
}

func extractSSRCsReport(report *rtcp.CCFeedbackReport) []uint32 {
	ssrcs := make([]uint32, len(report.ReportBlocks))
	for i, block := range report.ReportBlocks {
		ssrcs[i] = block.MediaSSRC
	}
	return ssrcs
}

func extractSSRCs(packet []byte) []uint32 {
	uniqueSSRCs := make(map[uint32]struct{})
	var ssrcs []uint32

	offset := 8
	for offset < len(packet)-4 {
		ssrc := binary.BigEndian.Uint32(packet[offset:])

		if _, ok := uniqueSSRCs[ssrc]; !ok {
			ssrcs = append(ssrcs, ssrc)
			uniqueSSRCs[ssrc] = struct{}{}
		}

		numReports := binary.BigEndian.Uint16(packet[offset+6:])

		// pad 16 bits 0 if numReports is not a multiple of 2
		if numReports%2 != 0 {
			numReports++
		}
		offset += 2 * int(numReports) // 2 bytes per report
		offset += 8                   // 4 byte SSRC + 2 bytes begin_seq + 2 bytes num_reports
	}

	return ssrcs
}

// BindLocalStream lets you modify any outgoing RTP packets. It is called once for per LocalStream. The returned method
// will be called once per rtp packet.
func (s *SenderInterceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {

	s.m.Lock()
	defer s.m.Unlock()

	if s.isClosed() {
		return writer
	}

	s.wg.Add(1)

	rtpQueue := s.newRTPQueue()
	localStream := &localStream{
		queue:       rtpQueue,
		newFrame:    make(chan struct{}),
		newFeedback: make(chan struct{}),
	}

	// TODO: Somehow set these attributes per stream
	priority := float64(1) // highest priority
	minBitrate := s.minBitrate
	startBitrate := s.initialBitrate
	maxBitrate := s.maxBitrate

	initialized := false

	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		if !initialized {
			s.rtpStreamsMu.Lock()
			s.rtpStreams[info.SSRC] = localStream
			s.rtpStreamsMu.Unlock()
			s.tx.RegisterNewStream(rtpQueue, info.SSRC, priority, minBitrate, startBitrate, maxBitrate)
			go s.loopPacingTimer(writer, info.SSRC)
			initialized = true
		}
		now := time.Now()
		t := s.getTimeNTP(now)

		buf := make([]byte, len(payload))
		copy(buf, payload)
		pkt := &rtp.Packet{Header: header.Clone(), Payload: buf}

		rtpQueue.Enqueue(&packet{
			rtp:        pkt,
			timestamp:  now,
			attributes: attributes,
		}, float64(t)/65536.0)
		size := pkt.MarshalSize()
		s.m.Lock()
		s.tx.NewMediaFrame(t, header.SSRC, size)
		s.m.Unlock()
		localStream.newFrame <- struct{}{}
		return size, nil
	})
}

// UnbindLocalStream is called when the Stream is removed. It can be used to clean up any data related to that track.
func (s *SenderInterceptor) UnbindLocalStream(info *interceptor.StreamInfo) {
	s.rtpStreamsMu.Lock()
	defer s.rtpStreamsMu.Unlock()
	close(s.rtpStreams[info.SSRC].close)
	delete(s.rtpStreams, info.SSRC)
}

// Close closes the interceptor
func (s *SenderInterceptor) Close() error {
	defer s.wg.Wait()
	s.m.Lock()
	defer s.m.Unlock()

	if !s.isClosed() {
		close(s.close)
	}
	return nil
}

func (s *SenderInterceptor) loopPacingTimer(writer interceptor.RTPWriter, ssrc uint32) {
	defer s.wg.Done()
	s.rtpStreamsMu.Lock()
	stream := s.rtpStreams[ssrc]
	s.rtpStreamsMu.Unlock()

	s.log.Infof("start send loop for ssrc: %v\n", ssrc)
	defer s.log.Infof("leave send loop for ssrc: %v", ssrc)

	timer := make(chan struct{})
	timerSet := false
	for {
		select {
		case <-stream.newFeedback:
			if timerSet {
				continue
			}
		case <-stream.newFrame:
			if timerSet {
				continue
			}
		case <-timer:
			timerSet = false
			timer = make(chan struct{})
		case <-s.close:
			return
		}

		if stream.queue.SizeOfQueue() <= 0 {
			continue
		}

		for {
			s.m.Lock()
			transmit := s.tx.IsOkToTransmit(s.getTimeNTP(time.Now()), ssrc)
			s.m.Unlock()

			if transmit == -1 {
				// no packets or CWND too small
				break
			}

			if transmit <= 1e-3 {
				// send packet
				packet := stream.queue.Dequeue()
				if packet == nil {
					break
				}
				if _, err := writer.Write(&packet.rtp.Header, packet.rtp.Payload, packet.attributes); err != nil {
					s.log.Warnf("failed sending RTP packet: %+v", err)
				}
				s.m.Lock()
				s.tx.AddTransmitted(s.getTimeNTP(time.Now()), ssrc, packet.rtp.MarshalSize(), packet.rtp.SequenceNumber, packet.rtp.Marker)
				s.m.Unlock()
			}

			if transmit > 1e-3 {
				go func() {
					d := time.Duration(1000*transmit) * time.Millisecond
					t := time.AfterFunc(d, func() {
						close(timer)
					})
					<-timer
					t.Stop()
				}()
				timerSet = true
				break
			}
		}
	}
}

// GetTargetBitrate returns the target bitrate calculated by SCReAM in bps.
func (s *SenderInterceptor) GetTargetBitrate(ssrc uint32) (int, error) {
	s.rtpStreamsMu.Lock()
	_, ok := s.rtpStreams[ssrc]
	s.rtpStreamsMu.Unlock()
	if !ok {
		return 0, fmt.Errorf("unknown SSRC, the stream may be unsupported or not yet registered")
	}

	s.m.Lock()
	defer s.m.Unlock()
	return int(s.tx.GetTargetBitrate(ssrc)), nil
}

func (s *SenderInterceptor) GetStats() map[string]interface{} {
	stats := s.tx.GetStatistics(s.getTimeNTP(time.Now())/65536.0, false)
	statSlice := strings.Split(stats, ",")
	keys := []string{
		"logTag",
		"queueDelay",
		"queueDelayMax",
		"queueDelayMinAvg",
		"sRTT",
		"cwnd",
		"bytesInFlightLog",
		"rateTransmittedConnection",
		"isInFastStart",
		"rtpQueueDelayStream0",
		"rtpQueueBytesInQueueStream0",
		"rtpQueueSizeOfQueueStream0",
		"targetBitrateStream0",
		"rateRTPStream0",
		"packetsRTPStream0",
		"rateTransmittedStream0",
		"rateAckedStream0",
		"rateLostStream0",
		"rateCEStream0",
		"packetsCEStream0",
		"hiSeqAckStream0",
		"clearedStream0",
		"packetsLostStream0",
	}
	res := make(map[string]interface{})
	for i := 0; i < len(statSlice) && i < len(keys); i++ {
		val := strings.TrimSpace(statSlice[i])
		res[keys[i]] = val
	}
	return res
}

func (s *SenderInterceptor) isClosed() bool {
	select {
	case <-s.close:
		return true
	default:
		return false
	}
}
