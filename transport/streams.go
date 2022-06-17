package transport

import (
	"context"

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

func (s *Stream) AddFlow(f *RTPFlow) {
	f.Bind(s)
}

func (s *Stream) Write(buf []byte) (int, error) {
	stream, err := s.conn.OpenUniStream()
	if err != nil {
		return 0, err
	}
	n, err := stream.Write(buf)
	if err != nil {
		return n, err
	}
	return n, stream.Close()
}

func (s *Stream) Read(buf []byte) (int, error) {
	stream, err := s.conn.AcceptUniStream(context.Background())
	if err != nil {
		return 0, err
	}
	return stream.Read(buf)
}
