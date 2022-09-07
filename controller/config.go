package controller

import (
	"errors"

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

type Option[T BaseServer] func(*T) error

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

func MTU[T BaseServer](mtu uint) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.mtu = mtu
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetAddr[T BaseServer](addr string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.addr = addr
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetTCPCongestionControlAlgorithm[T BaseServer](a CongestionControlAlgorithm) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.tcpCC = a
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetQUICCongestionControlAlgorithm[T BaseServer](a CongestionControlAlgorithm) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.quicCC = a
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetRTCPLogFileName[T BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.rtcpLogFileName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetRTPLogFileName[T BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.rtpLogFileName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func EnableStream[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.stream = true
		default:
			return errInvalidOption
		}
		return nil
	}
}

func DisableStream[T BaseServer]() Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.stream = false
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetSSLKeyLogFileName[T BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.sslKeyLogFileName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetQLOGDirName[T BaseServer](name string) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.qlogDirName = name
		default:
			return errInvalidOption
		}
		return nil
	}
}

func SetStream[T BaseServer](b bool) Option[T] {
	return func(s *T) error {
		switch x := any(s).(type) {
		case *BaseServer:
			x.stream = b
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
