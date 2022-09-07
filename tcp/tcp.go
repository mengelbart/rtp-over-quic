package tcp

import (
	"fmt"
	"net"
	"syscall"

	"github.com/mengelbart/rtp-over-quic/cc"
)

func connectTCP(addr string, cc cc.Algorithm) (*net.TCPConn, error) {
	dialer := &net.Dialer{
		Control: func(_, _ string, c syscall.RawConn) error {
			var operr error
			if err := c.Control(func(fd uintptr) {
				operr = syscall.SetsockoptString(int(fd), syscall.IPPROTO_TCP, syscall.TCP_CONGESTION, cc.String())
			}); err != nil {
				return err
			}
			if operr != nil {
				return fmt.Errorf("failed to set TCP congestion control algorithm to '%v': %w", cc.String(), operr)
			}
			return nil
		},
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	// TODO: Use something like this for tcp cwnd logging:
	//go func() {
	//	t := time.NewTicker(200 * time.Millisecond)
	//	for range t.C {
	//		conn, ok := conn.(interface {
	//			SyscallConn() (syscall.RawConn, error)
	//		})
	//		if !ok {
	//			panic(errors.New("doesn't have a SyscallConn"))
	//		}
	//		rawConn, err := conn.SyscallConn()
	//		if err != nil {
	//			panic(fmt.Errorf("couldn't get syscall.RawConn: %w", err))
	//		}
	//		rawConn.Control(func(fd uintptr) {
	//			info, serr := unix.GetsockoptTCPInfo(int(fd), unix.SOL_TCP, unix.TCP_INFO)
	//			if serr != nil {
	//				panic(serr)
	//			}
	//			fmt.Printf("%v\n", info.Snd_cwnd)
	//		})
	//	}
	//}()
	return conn.(*net.TCPConn), nil
}
