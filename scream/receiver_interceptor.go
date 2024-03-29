package scream

import (
	"sync"
	"time"

	"github.com/mengelbart/scream-go"
	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// ReceiverInterceptorFactory is a interceptor.Factory for a scream receiver
// interceptor
type ReceiverInterceptorFactory struct {
	opts []ReceiverOption
}

// NewInterceptor constructs a new SCReAM receiver interceptor
func (f *ReceiverInterceptorFactory) NewInterceptor(id string) (interceptor.Interceptor, error) {
	r := &ReceiverInterceptor{
		interval: time.Millisecond * 10,
		close:    make(chan struct{}),
		log:      logging.NewDefaultLoggerFactory().NewLogger("scream_receiver"),
		screamRx: map[uint32]*scream.Rx{},
		receive:  make(chan *packet),
		mark:     false,
	}
	for _, opt := range f.opts {
		if err := opt(r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// NewReceiverInterceptor returns a new ReceiverInterceptor
func NewReceiverInterceptor(opts ...ReceiverOption) (*ReceiverInterceptorFactory, error) {
	return &ReceiverInterceptorFactory{opts}, nil
}

type packet struct {
	rtp        *rtp.Packet
	timestamp  time.Time
	attributes interceptor.Attributes
}

// ReceiverInterceptor generates Feedback for SCReAM congestion control
type ReceiverInterceptor struct {
	interceptor.NoOp
	m     sync.Mutex
	wg    sync.WaitGroup
	close chan struct{}
	log   logging.LeveledLogger

	screamRx   map[uint32]*scream.Rx
	screamRxMu sync.Mutex
	interval   time.Duration
	receive    chan *packet

	mark bool // only for debugging purpose, setting this triggers a bug in some version s of the SCReAM receiver
}

func (r *ReceiverInterceptor) getTimeNTP(t time.Time) uint64 {
	return uint64(ntpTime32(t))
}

// BindRTCPWriter lets you modify any outgoing RTCP packets. It is called once per PeerConnection. The returned method
// will be called once per packet batch.
func (r *ReceiverInterceptor) BindRTCPWriter(writer interceptor.RTCPWriter) interceptor.RTCPWriter {
	r.m.Lock()
	defer r.m.Unlock()

	if r.isClosed() {
		return writer
	}

	r.wg.Add(1)

	go r.loop(writer)

	return writer
}

// BindRemoteStream lets you modify any incoming RTP packets. It is called once for per RemoteStream. The returned method
// will be called once per rtp packet.
func (r *ReceiverInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	if !streamSupportSCReAM(info) {
		return reader
	}

	rx := scream.NewRx(info.SSRC)
	r.screamRxMu.Lock()
	r.screamRx[info.SSRC] = rx
	r.screamRxMu.Unlock()

	return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		timestamp := time.Now()
		if ts, ok := a["timestamp"]; ok {
			if t, ok := ts.(time.Time); ok {
				timestamp = t
			}
		}
		i, attr, err := reader.Read(b, a)
		if err != nil {
			return 0, nil, err
		}
		buf := make([]byte, i)
		copy(buf, b)
		pkt := rtp.Packet{}
		if err = pkt.Unmarshal(buf); err != nil {
			return 0, nil, err
		}

		r.receive <- &packet{
			rtp:       &pkt,
			timestamp: timestamp,
		}

		return i, attr, nil
	})
}

// UnbindRemoteStream is called when the Stream is removed. It can be used to clean up any data related to that track.
func (r *ReceiverInterceptor) UnbindRemoteStream(info *interceptor.StreamInfo) {
	r.screamRxMu.Lock()
	delete(r.screamRx, info.SSRC)
	r.screamRxMu.Unlock()
}

// Close closes the interceptor.
func (r *ReceiverInterceptor) Close() error {
	defer r.wg.Wait()
	r.m.Lock()
	defer r.m.Unlock()

	if !r.isClosed() {
		close(r.close)
	}
	return nil
}

func (r *ReceiverInterceptor) loopFeedbackSender(rtcpWriter interceptor.RTCPWriter) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case now := <-ticker.C:
			r.screamRxMu.Lock()
			for _, rx := range r.screamRx {
				t := r.getTimeNTP(now)
				if ok, feedback := rx.CreateStandardizedFeedback(t, true); ok {
					var fb rtcp.CCFeedbackReport
					if err := fb.Unmarshal(feedback); err != nil {
						r.log.Warnf("failed to unmarshal scream feedback report: %+v", err)
					}
					if _, err := rtcpWriter.Write([]rtcp.Packet{&fb}, interceptor.Attributes{}); err != nil {
						r.log.Warnf("failed sending scream feedback report: %+v", err)
					}
				}
			}
			r.screamRxMu.Unlock()
		case <-r.close:
			return
		}
	}
}

func (r *ReceiverInterceptor) loop(rtcpWriter interceptor.RTCPWriter) {
	defer r.wg.Done()

	select {
	case <-r.close:
		return
	case pkt := <-r.receive:
		t := r.getTimeNTP(pkt.timestamp)

		r.screamRxMu.Lock()
		if rx, ok := r.screamRx[pkt.rtp.SSRC]; ok {
			//fmt.Printf("receive pkt %v at t=%v\n", pkt.SequenceNumber, t)
			rx.Receive(t, pkt.rtp.SSRC, pkt.rtp.MarshalSize(), pkt.rtp.SequenceNumber, 0)
		}
		r.screamRxMu.Unlock()
	}

	r.wg.Add(1)
	go r.loopFeedbackSender(rtcpWriter)

	lastFeedback := time.Now()
	for {
		select {
		case pkt := <-r.receive:
			t := r.getTimeNTP(pkt.timestamp)

			r.screamRxMu.Lock()
			if rx, ok := r.screamRx[pkt.rtp.SSRC]; ok {
				//fmt.Printf("receive pkt %v at t=%v\n", pkt.SequenceNumber, t)
				rx.Receive(t, pkt.rtp.SSRC, pkt.rtp.MarshalSize(), pkt.rtp.SequenceNumber, 0)
			}
			r.screamRxMu.Unlock()

			now := time.Now()
			if now.Sub(lastFeedback) > r.interval {
				r.screamRxMu.Lock()
				for _, rx := range r.screamRx {
					t := r.getTimeNTP(now)
					if ok, feedback := rx.CreateStandardizedFeedback(t, pkt.rtp.Marker); ok {
						var fb rtcp.CCFeedbackReport
						if err := fb.Unmarshal(feedback); err != nil {
							r.log.Warnf("failed to unmarshal scream feedback report: %+v", err)
						}
						if _, err := rtcpWriter.Write([]rtcp.Packet{&fb}, interceptor.Attributes{}); err != nil {
							r.log.Warnf("failed sending scream feedback report: %+v", err)
						}
					}
				}
				r.screamRxMu.Unlock()
				lastFeedback = now
			}

		case <-r.close:
			return
		}
	}
}

func (r *ReceiverInterceptor) isClosed() bool {
	select {
	case <-r.close:
		return true
	default:
		return false
	}
}
