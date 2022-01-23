package rtc

import (
	"fmt"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type rtpFormatter struct {
	seqnr unwrapper
}

func (f *rtpFormatter) rtpFormat(pkt *rtp.Packet, _ interceptor.Attributes) string {
	var twcc rtp.TransportCCExtension
	unwrappedSeqNr := f.seqnr.unwrap(pkt.SequenceNumber)
	if len(pkt.GetExtensionIDs()) > 0 {
		ext := pkt.GetExtension(pkt.GetExtensionIDs()[0])
		if err := twcc.Unmarshal(ext); err != nil {
			panic(err)
		}
		return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v, %v, %v\n",
			time.Now().UnixMilli(),
			pkt.PayloadType,
			pkt.SSRC,
			pkt.SequenceNumber,
			pkt.Timestamp,
			pkt.Marker,
			pkt.MarshalSize(),
			twcc.TransportSequence,
			unwrappedSeqNr,
		)
	}
	return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v, %v\n",
		time.Now().UnixMilli(),
		pkt.PayloadType,
		pkt.SSRC,
		pkt.SequenceNumber,
		pkt.Timestamp,
		pkt.Marker,
		pkt.MarshalSize(),
		unwrappedSeqNr,
	)
}

func rtcpFormat(pkts []rtcp.Packet, _ interceptor.Attributes) string {
	now := time.Now().UnixMilli()
	size := 0
	for _, pkt := range pkts {
		switch feedback := pkt.(type) {
		case *rtcp.TransportLayerCC:
			size += int(feedback.Len())
		case *rtcp.RawPacket:
			size += int(len(*feedback))
		}
	}
	return fmt.Sprintf("%v, %v\n", now, size)
}
