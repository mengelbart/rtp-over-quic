package controller

import "github.com/pion/interceptor"

type CommonConfig struct {
	addr              string
	quicCC            CongestionControlAlgorithm
	qlogDirName       string
	sslKeyLogFileName string

	rtpCC        CongestionControlAlgorithm
	localRFC8888 bool

	stream bool

	rtpLogFileName  string
	rtcpLogFileName string
	ccLogFileName   string
}

func newDefaultConfig() *CommonConfig {
	return &CommonConfig{
		addr:              ":4242",
		quicCC:            0,
		qlogDirName:       "",
		sslKeyLogFileName: "",
		rtpCC:             0,
		localRFC8888:      false,
		stream:            false,
		rtpLogFileName:    "",
		rtcpLogFileName:   "",
		ccLogFileName:     "",
	}
}

type Option func(*CommonConfig) error

func SetCCLogFileName(name string) Option {
	return func(cc *CommonConfig) error {
		cc.ccLogFileName = name
		return nil
	}
}

func SetRTCPLogFileName(name string) Option {
	return func(cc *CommonConfig) error {
		cc.rtcpLogFileName = name
		return nil
	}
}

func SetRTPLogFileName(name string) Option {
	return func(cc *CommonConfig) error {
		cc.rtpLogFileName = name
		return nil
	}
}

func EnableStream() Option {
	return func(cc *CommonConfig) error {
		cc.stream = true
		return nil
	}
}

func DisableStream() Option {
	return func(cc *CommonConfig) error {
		cc.stream = false
		return nil
	}
}

func EnableLocalRFC8888() Option {
	return func(cc *CommonConfig) error {
		cc.localRFC8888 = true
		return nil
	}
}

func DisableLocalRFC8888() Option {
	return func(cc *CommonConfig) error {
		cc.localRFC8888 = false
		return nil
	}
}

func SetRTPCongestionControlAlgorithm(algorithm CongestionControlAlgorithm) Option {
	return func(cc *CommonConfig) error {
		cc.rtpCC = algorithm
		return nil
	}
}

func SetSSLKeyLogFileName(name string) Option {
	return func(cc *CommonConfig) error {
		cc.sslKeyLogFileName = name
		return nil
	}
}

func SetQLOGDirName(name string) Option {
	return func(cc *CommonConfig) error {
		cc.qlogDirName = name
		return nil
	}
}

func SetStream(b bool) Option {
	return func(cc *CommonConfig) error {
		cc.stream = true
		return nil
	}
}

func SetAddr(addr string) Option {
	return func(cc *CommonConfig) error {
		cc.addr = addr
		return nil
	}
}

func (c *CommonConfig) registerPacketLog(registry *interceptor.Registry) error {
	rtpDumpFile, err := getLogFile(c.rtpLogFileName)
	if err != nil {
		return err
	}
	rtcpDumpFile, err := getLogFile(c.rtcpLogFileName)
	if err != nil {
		return err
	}
	return registerRTPSenderDumper(registry, rtpDumpFile, rtcpDumpFile)
}
