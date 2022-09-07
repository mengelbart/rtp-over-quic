package controller

import (
	"net"
)

func listenTCP(addr string) (*net.TCPListener, error) {
	// TODO: Setup CC alogithm?
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	return net.ListenTCP("tcp", tcpAddr)
}
