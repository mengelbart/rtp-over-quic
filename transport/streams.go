package transport

import (
	"github.com/lucas-clemente/quic-go"
)

type Stream struct {
	conn quic.Connection
}

func NewStreamTransportWithConn(conn quic.Connection) *Stream {
	return &Stream{
		conn: conn,
	}
}

func (t *Stream) AddFlow(f *Flow) {
	f.Bind(t)
}

func (t *Stream) Write(buf []byte) (int, error) {
	stream, err := t.conn.OpenUniStream()
	if err != nil {
		return 0, err
	}
	n, err := stream.Write(buf)
	if err != nil {
		return n, err
	}
	return n, stream.Close()
}
