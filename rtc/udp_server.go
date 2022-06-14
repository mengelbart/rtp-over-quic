package rtc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const desiredReceiveBufferSize = (1 << 20) * 2 // 2 MB

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
	if err = SetReceiveBuffer(conn); err != nil {
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

func (s *UDPServer) Close() error {
	return s.conn.Close()
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

func (t *udpTransport) SendMessage(msg []byte, _ func(error), _ func(bool)) error {
	_, err := t.conn.WriteTo(msg, t.addr)
	return err
}

func (t *udpTransport) CloseWithError(code int, msg string) error {
	// TODO
	return nil
}

func SetReceiveBuffer(c net.PacketConn) error {
	conn, ok := c.(interface{ SetReadBuffer(int) error })
	if !ok {
		return errors.New("connection doesn't allow setting of receive buffer size. Not a *net.UDPConn?")
	}
	size, err := inspectReadBuffer(c)
	if err != nil {
		return fmt.Errorf("failed to determine receive buffer size: %w", err)
	}
	if size >= desiredReceiveBufferSize {
		log.Printf("Conn has receive buffer of %d kiB (wanted: at least %d kiB)", size/1024, desiredReceiveBufferSize/1024)
		return nil
	}
	if err = conn.SetReadBuffer(desiredReceiveBufferSize); err != nil {
		return fmt.Errorf("failed to increase receive buffer size: %w", err)
	}
	newSize, err := inspectReadBuffer(c)
	if err != nil {
		return fmt.Errorf("failed to determine receive buffer size: %w", err)
	}
	if newSize == size {
		return fmt.Errorf("failed to increase receive buffer size (wanted: %d kiB, got %d kiB)", desiredReceiveBufferSize/1024, newSize/1024)
	}
	if newSize < desiredReceiveBufferSize {
		return fmt.Errorf("failed to sufficiently increase receive buffer size (was: %d kiB, wanted: %d kiB, got: %d kiB)", size/1024, desiredReceiveBufferSize/1024, newSize/1024)
	}
	log.Printf("Increased receive buffer size to %d kiB", newSize/1024)
	return nil
}

func inspectReadBuffer(c interface{}) (int, error) {
	conn, ok := c.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return 0, errors.New("doesn't have a SyscallConn")
	}
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("couldn't get syscall.RawConn: %w", err)
	}
	var size int
	var serr error
	if err := rawConn.Control(func(fd uintptr) {
		size, serr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
	}); err != nil {
		return 0, err
	}
	return size, serr
}
