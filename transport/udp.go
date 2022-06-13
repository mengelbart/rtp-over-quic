package transport

import "net"

type UDP struct {
	*net.UDPConn
}

func NewUDPTransportWithConn(conn *net.UDPConn) *UDP {
	return &UDP{
		conn,
	}
}

func (u *UDP) AddFlow(f *Flow) {
	f.Bind(u)
}
