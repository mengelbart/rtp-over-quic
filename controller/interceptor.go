package controller

import (
	"io"
	"time"

	"github.com/mengelbart/rtp-over-quic/scream"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/twcc"
	"github.com/pion/interceptor/rfc8888/pkg/rfc8888"
)

const feedbackInterval = 10 * time.Millisecond

func registerRTPSenderDumper(r *interceptor.Registry, rtp, rtcp io.Writer) error {
	rf := &rtpFormatter{}
	rtpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTPFormatter(rf.rtpFormat),
		packetdump.RTPWriter(rtp),
	)
	if err != nil {
		return err
	}

	rtcpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(rtcp),
	)
	if err != nil {
		return err
	}
	r.Add(rtpDumperInterceptor)
	r.Add(rtcpDumperInterceptor)
	return nil
}

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

func registerTWCCHeaderExtension(r *interceptor.Registry) error {
	headerExtension, err := twcc.NewHeaderExtensionInterceptor()
	if err != nil {
		return err
	}
	r.Add(headerExtension)
	return nil
}

func registerGCC(r *interceptor.Registry, cb cc.NewPeerConnectionCallback) error {
	fx := func() (cc.BandwidthEstimator, error) {
		return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(100_000), gcc.SendSideBWEPacer(gcc.NewLeakyBucketPacer(100_000)))
	}
	gccFactory, err := cc.NewInterceptor(fx)
	if err != nil {
		return err
	}
	gccFactory.OnNewPeerConnection(cb)
	r.Add(gccFactory)
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

func registerSCReAM(r *interceptor.Registry, cb scream.NewPeerConnectionCallback, initialBitrate int) error {
	var tx *scream.SenderInterceptorFactory
	tx, err := scream.NewSenderInterceptor(
		scream.InitialBitrate(float64(initialBitrate)),
		scream.MinBitrate(100_000),
	)
	if err != nil {
		return err
	}
	tx.OnNewPeerConnection(cb)
	r.Add(tx)
	return nil
}
