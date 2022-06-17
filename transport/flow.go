package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

var errInvalidTransport = errors.New("transport does not implement ack/loss callback")

// TODO: Implement flow without using flow id
type flow struct {
	transport io.Writer

	useID    bool
	id       uint64
	varIntID []byte
}

func newFlow() *flow {
	return &flow{
		transport: nil,
		useID:     false,
		id:        0,
		varIntID:  []byte{},
	}
}

func newFlowWithID(id uint64) *flow {
	var buf bytes.Buffer
	idWriter := quicvarint.NewWriter(&buf)
	quicvarint.Write(idWriter, id)
	return &flow{
		transport: nil,
		useID:     true,
		id:        id,
		varIntID:  buf.Bytes(),
	}
}

func (f *flow) write(payload []byte) (int, error) {
	if f.useID {
		payload = append(f.varIntID, payload...)
	}
	return f.transport.Write(payload)
}

func (f *flow) writeWithCallBack(payload []byte, callback func(bool)) (int, error) {
	if f.useID {
		payload = append(f.varIntID, payload...)
	}

	switch t := f.transport.(type) {
	case *Dgram:
		return t.WriteWithAckLossCallback(payload, callback)
	case *Stream:
		return t.WriteWithAckLossCallback(payload, callback)
	default:
		return 0, errInvalidTransport
	}
}

type RTCPFlow struct {
	*flow
}

func NewRTCPFlow() *RTCPFlow {
	return &RTCPFlow{
		flow: newFlow(),
	}
}

func NewRTCPFlowWithID(id uint64) *RTCPFlow {
	return &RTCPFlow{
		flow: newFlowWithID(id),
	}
}

func (f *RTCPFlow) Bind(t io.Writer) {
	f.transport = t
}

func (f *RTCPFlow) Write(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
	buf, err := rtcp.Marshal(pkts)
	if err != nil {
		return 0, err
	}
	return f.write(buf)
}

type RTPFlow struct {
	*flow
	localFeedback *localRFC8888Generator
}

func NewRTPFlow() *RTPFlow {
	return &RTPFlow{
		flow: newFlow(),
	}
}

func NewRTPFlowWithID(id uint64) *RTPFlow {
	return &RTPFlow{
		flow: newFlowWithID(id),
	}
}

func (f *RTPFlow) RunLocalFeedback(ctx context.Context, ssrc uint32, m Metricer, reportCB func(Feedback)) {
	f.localFeedback = newLocalRFC8888Generator(ssrc, m, reportCB)
	go f.localFeedback.Run(ctx)
}

func (f *RTPFlow) Bind(t io.Writer) {
	f.transport = t
}

func (f *RTPFlow) Write(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
	headerBuf, err := header.Marshal()
	if err != nil {
		return 0, err
	}
	if f.localFeedback != nil {
		return f.writeWithCallBack(
			append(headerBuf, payload...),
			f.ackCallback(
				time.Now(),
				header.SSRC,
				header.MarshalSize(),
				header.SequenceNumber,
			),
		)
	}
	return f.write(append(headerBuf, payload...))
}

func (f *RTPFlow) ackCallback(sent time.Time, ssrc uint32, size int, seqNr uint16) func(bool) {
	return func(b bool) {
		if b {
			f.localFeedback.ack(ackedPkt{
				sentTS: sent,
				ssrc:   ssrc,
				size:   size,
				seqNr:  seqNr,
			})
		}
	}
}
