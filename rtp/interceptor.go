package rtp

import (
	"io"
	"time"

	"github.com/mengelbart/rtp-over-quic/logging"
	"github.com/mengelbart/rtp-over-quic/scream"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/rfc8888"
	"github.com/pion/interceptor/pkg/twcc"
)

const feedbackInterval = 10 * time.Millisecond

type Option func(*interceptor.Registry) error

func New(options ...Option) (interceptor.Interceptor, error) {
	registry := interceptor.Registry{}
	for _, option := range options {
		if err := option(&registry); err != nil {
			return nil, err
		}
	}
	i, err := registry.Build("")
	if err != nil {
		return nil, err
	}
	return i, nil
}

func RegisterSenderPacketLog(rtpLogFileName, rtcpLogFileName string) Option {
	return func(r *interceptor.Registry) error {
		rtpDumpFile, err := logging.GetLogFile(rtpLogFileName)
		if err != nil {
			return err
		}
		rtcpDumpFile, err := logging.GetLogFile(rtcpLogFileName)
		if err != nil {
			return err
		}
		return registerRTPSenderDumper(r, rtpDumpFile, rtcpDumpFile)
	}
}

func RegisterReceiverPacketLog(rtpLogFileName, rtcpLogFileName string) Option {
	return func(r *interceptor.Registry) error {
		rtpDumpFile, err := logging.GetLogFile(rtpLogFileName)
		if err != nil {
			return err
		}
		rtcpDumpFile, err := logging.GetLogFile(rtcpLogFileName)
		if err != nil {
			return err
		}
		return registerRTPReceiverDumper(r, rtpDumpFile, rtcpDumpFile)
	}
}

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

func RegisterTWCC() Option {
	return func(r *interceptor.Registry) error {
		fbFactory, err := twcc.NewSenderInterceptor(twcc.SendInterval(feedbackInterval))
		if err != nil {
			return err
		}
		r.Add(fbFactory)
		return nil
	}
}

func RegisterTWCCHeaderExtension() Option {
	return func(r *interceptor.Registry) error {
		headerExtension, err := twcc.NewHeaderExtensionInterceptor()
		if err != nil {
			return err
		}
		r.Add(headerExtension)
		return nil
	}
}

func RegisterGCC(cb cc.NewPeerConnectionCallback) Option {
	return func(r *interceptor.Registry) error {
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
}

func RegisterRFC8888() Option {
	return func(r *interceptor.Registry) error {
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
}

func RegisterRFC8888Pion() Option {
	return func(r *interceptor.Registry) error {
		rx, err := rfc8888.NewSenderInterceptor(rfc8888.SendInterval(feedbackInterval))
		if err != nil {
			return err
		}
		r.Add(rx)
		return nil
	}
}

func RegisterSCReAM(cb scream.NewPeerConnectionCallback, initialBitrate int) Option {
	return func(r *interceptor.Registry) error {
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
}
