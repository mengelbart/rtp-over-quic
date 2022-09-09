package rtp

type AttributeKey int

const (
	RELIABILITY AttributeKey = iota
)

type Reliability bool

const (
	REQUIRED     Reliability = true
	NOT_REQUIRED Reliability = false
)
