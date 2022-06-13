package transport

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
)

type TCP struct {
	*net.TCPConn
}

func NewTCPTransportWithConn(conn *net.TCPConn) *TCP {
	return &TCP{
		conn,
	}
}

func (t *TCP) Write(msg []byte) (int, error) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(msg)))
	return t.TCPConn.Write(append(buf, msg...))
}

func (t *TCP) Read(buf []byte) (int, error) {
	prefix := make([]byte, 2)
	if _, err := io.ReadFull(t.TCPConn, prefix); err != nil {
		return 0, fmt.Errorf("failed to read length from TCP conn: %w, exiting", err)
	}
	length := binary.BigEndian.Uint16(prefix)
	tmp := make([]byte, length)
	if _, err := io.ReadFull(t.TCPConn, tmp); err != nil {
		return 0, fmt.Errorf("failed to read complete frame from TCP conn: %w, exiting", err)
	}
	n := copy(buf, tmp)
	if n < int(length) {
		log.Printf("WARNING: buffer too short, TCP reader dropped %v bytes", int(length)-n)
	}
	return n, nil
}

func (t *TCP) AddFlow(f *Flow) {
	f.Bind(t)
}
