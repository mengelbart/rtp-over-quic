package transport

import (
	"fmt"

	"github.com/lucas-clemente/quic-go"
)

type Dgram struct {
	conn quic.Connection
}

func NewDgramTransportWithConn(conn quic.Connection) *Dgram {
	return &Dgram{
		conn: conn,
	}
}

func (t *Dgram) Write(buf []byte) (int, error) {
	return len(buf), t.conn.SendMessage(buf, nil, nil)
}

func (t *Dgram) Read(buf []byte) (int, error) {
	msg, err := t.conn.ReceiveMessage()
	if err != nil {
		return 0, err
	}
	if len(buf) < len(msg) {
		return 0, fmt.Errorf("receive buffer to small, got %v but need %v", len(buf), len(msg))
	}
	n := copy(buf, msg)
	if n != len(msg) {
		return 0, fmt.Errorf("copied less bytes than received: copied %v, but received %v", n, len(msg))
	}
	return n, nil
}

func (t *Dgram) WriteWithAckLossCallback(buf []byte, cb func(bool)) (int, error) {
	return len(buf), t.conn.SendMessage(buf, nil, cb)
}

func (t *Dgram) AddFlow(f *Flow) {
	f.Bind(t)
}
