package transport

import (
	"bytes"
	"io"
	"time"

	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

type Flow struct {
	id        uint64
	varIntID  []byte
	transport io.Writer

	localFeedback *localRFC8888Generator
}

func NewFlowWithID(id uint64) *Flow {
	var buf bytes.Buffer
	idWriter := quicvarint.NewWriter(&buf)
	quicvarint.Write(idWriter, id)
	return &Flow{
		id:        id,
		varIntID:  buf.Bytes(),
		transport: nil,
	}
}

func (f *Flow) EnableLocalFeedback(ssrc uint32, m Metricer, reportCB func(Feedback)) {
	f.localFeedback = newLocalRFC8888Generator(ssrc, m, reportCB)
	go f.localFeedback.Run()
}

func (f *Flow) Bind(t io.Writer) {
	f.transport = t
}

func (f *Flow) Write(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
	headerBuf, err := header.Marshal()
	if err != nil {
		return 0, err
	}
	buf := make([]byte, len(f.varIntID)+len(headerBuf)+len(payload))
	copy(buf, f.varIntID)
	copy(buf[len(f.varIntID):], headerBuf)
	copy(buf[len(f.varIntID)+len(headerBuf):], payload)

	if f.localFeedback != nil {
		dgram := f.transport.(*Dgram)
		return dgram.WriteWithAckLossCallback(buf, f.ackCallback(time.Now(), header.SSRC, header.MarshalSize(), header.SequenceNumber))
	}
	return f.transport.Write(buf)
}

func (f *Flow) ackCallback(sent time.Time, ssrc uint32, size int, seqNr uint16) func(bool) {
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

func (f *Flow) Close() error {
	if f.localFeedback != nil {
		return f.localFeedback.Close()
	}
	return nil
}
