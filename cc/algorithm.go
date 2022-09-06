package cc

import "log"

type Algorithm int

const (
	Reno Algorithm = iota
	Cubic
	BBR
	SCReAM
	GCC
	NONE
)

func AlgorithmFromString(a string) Algorithm {
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
	case "none":
		return NONE
	default:
		log.Printf("warning, unknown algorithm: %v, using default ('reno')", a)
		return Reno
	}
}

func (a Algorithm) String() string {
	switch a {
	case Reno:
		return "reno"
	case Cubic:
		return "cubic"
	case BBR:
		return "bbr"
	case SCReAM:
		return "scream"
	case GCC:
		return "gcc"
	case NONE:
		return "none"
	default:
		log.Printf("warning, undefined algorithm: %v", int(a))
		return "none"
	}
}
