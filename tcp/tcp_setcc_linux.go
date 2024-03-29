//go:build linux
// +build linux

package tcp

import "syscall"

func setCC(fd int, algo string) error {
	return syscall.SetsockoptString(fd, syscall.IPPROTO_TCP, syscall.TCP_CONGESTION, algo)
}
