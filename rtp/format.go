package rtp

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
	var twccNr uint16
	if len(pkt.GetExtensionIDs()) > 0 {
		ext := pkt.GetExtension(pkt.GetExtensionIDs()[0])
		if err := twcc.Unmarshal(ext); err != nil {
			panic(err)
		}
		twccNr = twcc.TransportSequence
	}
	return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v, %v, %v\n",
		time.Now().UnixMilli(),
		pkt.PayloadType,
		pkt.SSRC,
		pkt.SequenceNumber,
		pkt.Timestamp,
		pkt.Marker,
		pkt.MarshalSize(),
		twccNr,
		unwrappedSeqNr,
	)
}

func rtcpFormat(pkts []rtcp.Packet, _ interceptor.Attributes) string {
	now := time.Now().UnixMilli()
	size := 0
	for _, pkt := range pkts {
		switch feedback := pkt.(type) {
		case *rtcp.CCFeedbackReport:
			size += int(feedback.Len())
			//return fmt.Sprintf("len=%v; %v\n", feedback.Len(), feedback)
		case *rtcp.TransportLayerCC:
			size += int(feedback.Len())
			//return fmt.Sprintf("len=%v; %v\n", feedback.Len(), feedback)
		case *rtcp.RawPacket:
			size += int(len(*feedback))
			//return fmt.Sprintf("RAW: %v\n%x\n", feedback.Header(), feedback)
		}
	}
	return fmt.Sprintf("%v, %v\n", now, size)
}
