package controller

import (
	"errors"
	"io"
	"log"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/pion/interceptor"
)

// demultiplexer inspects a packet buffer and returns the flow id to which the
// packet belongs. The second return value is the remaining part of the packet
// after removing the flow id from the packet. If there is no flow id stored in
// the packet, the original buffer should be returned without modification.
type demultiplexer interface {
	getFlow([]byte) (uint64, []byte, error)
}

type demultiplexerFunc func(pkt []byte) (uint64, []byte, error)

func (f demultiplexerFunc) getFlow(pkt []byte) (uint64, []byte, error) {
	return f(pkt)
}

type receiver struct {
	mtu uint

	// implementation
	demultiplexer
	lock          sync.Mutex
	mediaSinks    map[uint64]MediaSink
	incomingFlows map[uint64]interceptor.RTPReader

	// TODO: Use list to allow multiple parallel transports?
	transport io.ReadWriter
}

func newReceiver(mtu uint, d demultiplexer) *receiver {
	return &receiver{
		mtu:           mtu,
		demultiplexer: d,
		lock:          sync.Mutex{},
		mediaSinks:    map[uint64]MediaSink{},
		incomingFlows: map[uint64]interceptor.RTPReader{},
		transport:     nil,
	}
}

func (r *receiver) addIncomingFlow(id uint64, i interceptor.Interceptor, sink MediaSink, t io.ReadWriter) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.transport = t
	if s, ok := r.mediaSinks[id]; ok {
		if err := s.Stop(); err != nil {
			log.Printf("failed to close existing sink: %v", err)
		}
	}
	r.mediaSinks[id] = sink

	rtpReader := i.BindRemoteStream(&interceptor.StreamInfo{
		ID:                  "",
		Attributes:          map[interface{}]interface{}{},
		SSRC:                0,
		PayloadType:         0,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{{URI: transportCCURI, ID: 1}},
		MimeType:            "",
		ClockRate:           0,
		Channels:            0,
		SDPFmtpLine:         "",
		RTCPFeedback:        []interceptor.RTCPFeedback{{Type: "ack", Parameter: "ccfb"}},
	}, interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {

		n, err := sink.Write(b)
		if err != nil {
			return 0, nil, err
		}

		return n, a, nil
	}))
	r.incomingFlows[id] = rtpReader
}

func (r *receiver) receive() error {
	buf := make([]byte, r.mtu)

	for {
		n, err := r.transport.Read(buf)
		if err != nil {
			if e, ok := err.(*quic.ApplicationError); ok && e.ErrorCode == 0 {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		id, packet, err := r.getFlow(buf[:n])
		if err != nil {
			log.Printf("failed to demultiplex packet: %v, dropping packet", err)
			continue
		}
		r.lock.Lock()
		flow, ok := r.incomingFlows[id]
		r.lock.Unlock()
		if !ok {
			log.Printf("WARNING: no flow with ID %v found", id)
		}
		if !ok {
			log.Printf("got flow with unknown flow ID: %v, dropping datagram", id)
			continue
		}
		if _, _, err := flow.Read(packet, interceptor.Attributes{"timestamp": time.Now()}); err != nil {
			log.Printf("failed to read RTP packet: %v", err)
			return err
		}
	}
}
