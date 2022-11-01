//go:build !linux
// +build !linux

package tcp

import "log"

func setCC(_ int, _ string) error {
	// noop
	log.Printf("WARNING: setting CC algorithm for TCP is not supported on non-Linux platforms. Using default.")
	return nil
}
