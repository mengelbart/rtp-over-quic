package rtc

import (
	"io"
	"time"

	"github.com/mengelbart/rtp-over-quic/rtc/scream"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/twcc"
	"github.com/pion/interceptor/rfc8888/pkg/rfc8888"
)

const feedbackInterval = 10 * time.Millisecond

func registerRTPReceiverDumper(r *interceptor.Registry, rtp, rtcp io.Writer) error {
	rtcpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(rtcp),
	)
	if err != nil {
		return err
	}

	rf := &rtpFormatter{}
	rtpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTPFormatter(rf.rtpFormat),
		packetdump.RTPWriter(rtp),
	)
	if err != nil {
		return err
	}
	r.Add(rtcpDumperInterceptor)
	r.Add(rtpDumperInterceptor)
	return nil
}

func registerTWCC(r *interceptor.Registry) error {
	fbFactory, err := twcc.NewSenderInterceptor(twcc.SendInterval(feedbackInterval))
	if err != nil {
		return err
	}
	r.Add(fbFactory)
	return nil
}

func registerRFC8888(r *interceptor.Registry) error {
	var rx *scream.ReceiverInterceptorFactory
	rx, err := scream.NewReceiverInterceptor(
		scream.ReceiverInterval(feedbackInterval),
	)
	if err != nil {
		return err
	}
	r.Add(rx)
	return nil
}

func registerRFC8888Pion(r *interceptor.Registry) error {
	rx, err := rfc8888.NewSenderInterceptor(rfc8888.SendInterval(feedbackInterval))
	if err != nil {
		return err
	}
	r.Add(rx)
	return nil
}
