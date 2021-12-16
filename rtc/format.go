package rtc

import (
	"fmt"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

func rtpFormat(pkt *rtp.Packet, _ interceptor.Attributes) string {
	var twcc rtp.TransportCCExtension
	if len(pkt.GetExtensionIDs()) > 0 {
		ext := pkt.GetExtension(pkt.GetExtensionIDs()[0])
		if err := twcc.Unmarshal(ext); err != nil {
			panic(err)
		}
		return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v, %v\n",
			time.Now().UnixMilli(),
			pkt.PayloadType,
			pkt.SSRC,
			pkt.SequenceNumber,
			pkt.Timestamp,
			pkt.Marker,
			pkt.MarshalSize(),
			twcc.TransportSequence,
		)
	}
	return fmt.Sprintf("%v, %v, %v, %v, %v, %v, %v\n",
		time.Now().UnixMilli(),
		pkt.PayloadType,
		pkt.SSRC,
		pkt.SequenceNumber,
		pkt.Timestamp,
		pkt.Marker,
		pkt.MarshalSize(),
	)
}

func rtcpFormat(pkts []rtcp.Packet, _ interceptor.Attributes) string {
	res := fmt.Sprintf("%v\t", time.Now().UnixMilli())
	for _, pkt := range pkts {
		switch feedback := pkt.(type) {
		case *rtcp.TransportLayerCC:
			res += feedback.String()
		}
	}
	return res
}
