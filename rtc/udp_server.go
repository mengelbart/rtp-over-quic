package rtc

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

type UDPServer struct {
	conn         *net.UDPConn
	clients      map[string]*udpTransport
	makeReceiver ReceiverFactory
	sinkFactory  MediaSinkFactory
}

func NewUDPServer(f ReceiverFactory, addr string, sinkFactory MediaSinkFactory) (*UDPServer, error) {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", a)
	if err != nil {
		return nil, err
	}
	return &UDPServer{
		conn:         conn,
		clients:      map[string]*udpTransport{},
		makeReceiver: f,
		sinkFactory:  sinkFactory,
	}, nil
}

func (s *UDPServer) Listen(ctx context.Context) error {
	for {
		buf := make([]byte, 1500)
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			// TODO: Check if this is correct?
			return err
		}
		key := fmt.Sprintf("%v:%v", addr.IP.To16(), addr.Port)
		client, ok := s.clients[key]
		if !ok {
			client = &udpTransport{
				conn: s.conn,
				addr: addr,
				in:   make(chan []byte, 1000),
			}
			receiver, err := s.makeReceiver(client, s.sinkFactory)
			if err != nil {
				log.Printf("failed to create receiver: %v\n", err)
				continue
			}
			go func() {
				log.Println("starting receiver")
				defer receiver.Close()
				err := receiver.run(ctx)
				if err != nil {
					log.Printf("receiver closed connection: %v\n", err)
				}
				close(client.in)
			}()
			s.clients[key] = client
		}
		select {
		case client.in <- buf[:n]:
		default:
			log.Println("client buffer full, dropping message")
		}
	}
}

type udpTransport struct {
	conn *net.UDPConn
	addr *net.UDPAddr
	in   chan []byte
}

func (t *udpTransport) ReceiveMessage() ([]byte, error) {
	select {
	case msg := <-t.in:
		return msg, nil
	case <-time.After(4 * time.Second):
		return nil, fmt.Errorf("timeout")
	}
}

func (t *udpTransport) SendMessage(msg []byte) error {
	_, err := t.conn.WriteTo(msg, t.addr)
	return err
}

func (t *udpTransport) CloseWithError(code int, msg string) error {
	// TODO
	return nil
}

func (s *UDPServer) Close() error {
	return s.conn.Close()
}
