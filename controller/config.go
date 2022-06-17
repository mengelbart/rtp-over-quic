package controller

import (
	"errors"

	"github.com/mengelbart/rtp-over-quic/transport"
	"github.com/pion/interceptor"
)

var errInvalidOption = errors.New("invalid option type")

type commonBaseConfig struct {
	addr string

	mtu uint

	quicCC            CongestionControlAlgorithm
	qlogDirName       string
	sslKeyLogFileName string

	tcpCC CongestionControlAlgorithm

	stream bool

	rtpLogFileName  string
	rtcpLogFileName string
}

func (c *commonBaseConfig) registerSenderPacketLog(registry *interceptor.Registry) error {
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

func (c *commonBaseConfig) registerReceiverPacketLog(registry *interceptor.Registry) error {
	rtpDumpFile, err := getLogFile(c.rtpLogFileName)
	if err != nil {
		return err
	}
	rtcpDumpFile, err := getLogFile(c.rtcpLogFileName)
	if err != nil {
		return err
	}
	return registerRTPReceiverDumper(registry, rtpDumpFile, rtcpDumpFile)
}

type Option[T BaseSender | BaseServer] func(*T) error

// Generic options look a bit ugly due to this note from the go1.18 release
// notes:
//
// > The Go compiler does not support accessing a struct field x.f where x is of
// > type parameter type even if all types in the type parameter's type set have
// > a field f. We may remove this restriction in a future release.
// (https://tip.golang.org/doc/go1.18#generics)
//
// If this constrained will be removed at some point, we can clean this up
// again.

func MTU[T BaseSender | BaseServer](mtu uint) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.mtu = mtu
		case *BaseServer:
			x.mtu = mtu
		default:
			return errInvalidOption
		}
		return nil
	}
}

func EnableFlowID[T BaseSender](id uint64) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.flow = transport.NewRTPFlowWithID(id)
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetAddr[T BaseSender | BaseServer](addr string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.addr = addr
		case *BaseServer:
			x.addr = addr
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetTCPCongestionControlAlgorithm[T BaseSender | BaseServer](a CongestionControlAlgorithm) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.tcpCC = a
		case *BaseServer:
			x.tcpCC = a
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetQUICCongestionControlAlgorithm[T BaseSender | BaseServer](a CongestionControlAlgorithm) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.quicCC = a
		case *BaseServer:
			x.quicCC = a
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetRTCPLogFileName[T BaseSender | BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.rtcpLogFileName = name
		case *BaseServer:
			x.rtcpLogFileName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetRTPLogFileName[T BaseSender | BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.rtpLogFileName = name
		case *BaseServer:
			x.rtpLogFileName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func EnableStream[T BaseSender | BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.stream = true
		case *BaseServer:
			x.stream = true
		default:
			return errInvalidOption
		}
		return nil
	}
}

func DisableStream[T BaseSender | BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.stream = false
		case *BaseServer:
			x.stream = false
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetSSLKeyLogFileName[T BaseSender | BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.sslKeyLogFileName = name
		case *BaseServer:
			x.sslKeyLogFileName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetQLOGDirName[T BaseSender | BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.qlogDirName = name
		case *BaseServer:
			x.qlogDirName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetStream[T BaseSender | BaseServer](b bool) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.stream = b
		case *BaseServer:
			x.stream = b
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetCCLogFileName[T BaseSender](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.ccLogFileName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}
func EnableLocalRFC8888[T BaseSender]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.localRFC8888 = true
		default:
			return errInvalidOption
		}
		return nil
	}
}

func DisableLocalRFC8888[T BaseSender]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.localRFC8888 = false
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetRTPCongestionControlAlgorithm[T BaseSender](algorithm CongestionControlAlgorithm) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseSender:
			x.rtpCC = algorithm
		default:
			return errInvalidOption
		}
		return nil
	}
}

func EnableRFC8888[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.rfc8888 = true
		default:
			return errInvalidOption
		}
		return nil
	}
}

func DisbleRFC8888[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.rfc8888 = false
		default:
			return errInvalidOption
		}
		return nil
	}
}

func EnableRFC8888Pion[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.rfc8888Pion = true
		default:
			return errInvalidOption
		}
		return nil
	}
}

func DisbleRFC8888Pion[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.rfc8888Pion = false
		default:
			return errInvalidOption
		}
		return nil
	}
}

func EnableTWCC[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.twcc = true
		default:
			return errInvalidOption
		}
		return nil
	}
}

func DisbleTWCC[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.twcc = false
		default:
			return errInvalidOption
		}
		return nil
	}
}
