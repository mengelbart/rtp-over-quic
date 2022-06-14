package media

type ConfigOption func(*Config) error

type Config struct {
	targetBitrate uint
	ssrc          uint32
	mtu           uint
	payloadType   uint8
	clockRate     uint32
	codec         string
}

func newConfig(opts ...ConfigOption) (*Config, error) {
	c := &Config{
		targetBitrate: 100_000,
		ssrc:          0,
		mtu:           1500,
		payloadType:   96,
		clockRate:     90000,
		codec:         "h264",
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func InitialTargetBitrate(r uint) ConfigOption {
	return func(s *Config) error {
		s.targetBitrate = r
		return nil
	}
}

func SSRC(ssrc uint32) ConfigOption {
	return func(c *Config) error {
		c.ssrc = ssrc
		return nil
	}
}

func MTU(mtu uint) ConfigOption {
	return func(c *Config) error {
		c.mtu = mtu
		return nil
	}
}

func PayloadType(pt uint8) ConfigOption {
	return func(c *Config) error {
		c.payloadType = pt
		return nil
	}
}

func ClockRate(r uint32) ConfigOption {
	return func(c *Config) error {
		c.clockRate = r
		return nil
	}
}

func Codec(codec string) ConfigOption {
	return func(c *Config) error {
		c.codec = codec
		return nil
	}
}
