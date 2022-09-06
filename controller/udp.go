package controller

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const desiredReceiveBufferSize = (1 << 20) * 2 // 2 MB

func listenUDP(addr string) (*net.UDPConn, error) {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", a)
	if err != nil {
		return nil, err
	}
	if err = SetReceiveBuffer(conn); err != nil {
		if !strings.Contains(err.Error(), "use of closed network connection") {
			log.Printf("%s. See https://github.com/lucas-clemente/quic-go/wiki/UDP-Receive-Buffer-Size for details.", err)
		} else {
			return nil, err
		}
	}
	return conn, nil
}

func connectUDP(addr string) (*net.UDPConn, error) {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, a)
	if err != nil {
		return nil, err
	}
	if err = SetReceiveBuffer(conn); err != nil {
		if !strings.Contains(err.Error(), "use of closed network connection") {
			log.Printf("%s. See https://github.com/lucas-clemente/quic-go/wiki/UDP-Receive-Buffer-Size for details.", err)
		} else {
			return nil, err
		}
	}
	return conn, nil
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
