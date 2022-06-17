package controller

import (
	"github.com/pion/interceptor"
)

type BaseServer struct {
	commonBaseConfig

	rfc8888     bool
	rfc8888Pion bool
	twcc        bool

	mediaFactory MediaSinkFactory
}

func newBaseServer(mediaFactory MediaSinkFactory, opts ...Option[BaseServer]) (*BaseServer, error) {
	c := &BaseServer{
		commonBaseConfig: commonBaseConfig{
			addr:              ":4242",
			quicCC:            Reno,
			qlogDirName:       "",
			sslKeyLogFileName: "",
			tcpCC:             Cubic,
			stream:            false,
			rtpLogFileName:    "",
			rtcpLogFileName:   "",
		},
		rfc8888:      false,
		rfc8888Pion:  false,
		twcc:         false,
		mediaFactory: mediaFactory,
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (s *BaseServer) buildInterceptorRegistry() (*interceptor.Registry, error) {
	ir := &interceptor.Registry{}
	if len(s.rtpLogFileName) > 0 || len(s.rtcpLogFileName) > 0 {
		if err := s.registerReceiverPacketLog(ir); err != nil {
			return nil, err
		}
	}
	if s.rfc8888 {
		if err := registerRFC8888(ir); err != nil {
			return nil, err
		}
	}
	if s.rfc8888Pion {
		if err := registerRFC8888Pion(ir); err != nil {
			return nil, err
		}
	}
	if s.twcc {
		if err := registerTWCC(ir); err != nil {
			return nil, err
		}
	}
	return ir, nil
}
