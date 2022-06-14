package rtc

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
)

type TCPServer struct {
	listener     net.Listener
	makeReceiver ReceiverFactory
	sinkFactory  MediaSinkFactory
}

func NewTCPServer(rf ReceiverFactory, addr string, sf MediaSinkFactory) (*TCPServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &TCPServer{
		listener:     listener,
		makeReceiver: rf,
		sinkFactory:  sf,
	}, nil
}

func (s *TCPServer) Listen(ctx context.Context) error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}
		receiver, err := s.makeReceiver(&tcpTransport{
			conn: conn,
		}, s.sinkFactory)
		if err != nil {
			log.Printf("failed to create receiver: %v\n", err)
			continue
		}
		go func() {
			log.Println("starting receiver")
			defer receiver.Close()
			if err := receiver.run(ctx); err != nil {
				log.Printf("receiver closed connection: %v\n", err)
			}
		}()
	}
}

func (s *TCPServer) Close() error {
	return s.listener.Close()
}

type tcpTransport struct {
	conn net.Conn
}

func (t *tcpTransport) ReceiveMessage() ([]byte, error) {
	prefix := make([]byte, 2)
	if _, err := io.ReadFull(t.conn, prefix); err != nil {
		return nil, fmt.Errorf("failed to read length from TCP conn: %w, exiting", err)
	}
	length := binary.BigEndian.Uint16(prefix)
	buf := make([]byte, length)
	if _, err := io.ReadFull(t.conn, buf); err != nil {

		return nil, fmt.Errorf("failed to read complete frame from TCP conn: %w, exiting", err)
	}
	return buf, nil
}

func (t *tcpTransport) SendMessage(msg []byte, _ func(error), _ func(bool)) error {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(msg)))
	n, err := t.conn.Write(append(buf, msg...))
	if err != nil {
		return err
	}
	if n != len(msg)+2 {
		log.Fatalf("not enough bytes written to tcp conn: n=%v, expected=%v\n", n, len(msg)+2)
	}
	return nil
}

func (t *tcpTransport) CloseWithError(_ int, _ string) error {
	return t.conn.Close()
}
