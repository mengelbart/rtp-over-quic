package controller

import (
	"fmt"
	"net"
	"syscall"
)

func connectTCP(addr string, cc CongestionControlAlgorithm) (*net.TCPConn, error) {
	dialer := &net.Dialer{
		Control: func(_, _ string, c syscall.RawConn) error {
			var operr error
			if err := c.Control(func(fd uintptr) {
				operr = syscall.SetsockoptString(int(fd), syscall.IPPROTO_TCP, syscall.TCP_CONGESTION, cc.String())
			}); err != nil {
				return err
			}
			if operr != nil {
				return fmt.Errorf("failed to set TCP congestion control algorithm to '%v': %w", cc.String(), operr)
			}
			return nil
		},
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return conn.(*net.TCPConn), nil
}
