package controller

import "log"

type CongestionControlAlgorithm int

const (
	Reno CongestionControlAlgorithm = iota
	Cubic
	BBR
	SCReAM
	GCC
)

func CongestionControlAlgorithmFromString(a string) CongestionControlAlgorithm {
	switch a {
	case "reno":
		return Reno
	case "cubic":
		return Cubic
	case "bbr":
		return BBR
	case "scream":
		return SCReAM
	case "gcc":
		return GCC
	default:
		log.Printf("warning, unknown algorithm: %v, using default ('reno')", a)
		return Reno
	}
}
