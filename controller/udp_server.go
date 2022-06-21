package controller

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/mengelbart/rtp-over-quic/transport"
)

type udpPacket struct {
	buf  []byte
	addr *net.UDPAddr
	conn *net.UDPConn
}

type UDPServer struct {
	BaseServer
	routerChan chan *udpPacket
}

func NewUDPServer(mediaFactory MediaSinkFactory, opts ...Option[BaseServer]) (*UDPServer, error) {
	bs, err := newBaseServer(mediaFactory, opts...)
	if err != nil {
		return nil, err
	}
	return &UDPServer{
		BaseServer: *bs,
		routerChan: make(chan *udpPacket, 1000),
	}, nil
}

func (s *UDPServer) Start(ctx context.Context) error {
	conn, err := listenUDP(s.addr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		if err := conn.Close(); err != nil {
			log.Printf("failed to close UDP conn: %v", err)
		}
	}()

	var wg sync.WaitGroup

	buf := make([]byte, s.mtu)
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.route()
	}()

	defer func() {
		close(s.routerChan)
		wg.Wait()
	}()

	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("ERR: %v", err)
			return err
		}
		// TODO: Use buffer pool?
		pktBuf := make([]byte, n)
		copy(pktBuf, buf[:n])
		select {
		case s.routerChan <- &udpPacket{
			buf:  pktBuf,
			addr: addr,
			conn: conn,
		}:
		default:
			log.Printf("router buffer full, ignored packet")
		}
	}
}

type udpClientConnection struct {
	conn        *net.UDPConn
	addr        *net.UDPAddr
	dataCh      chan []byte
	readTimeout time.Duration
}

var errPeerLeft = errors.New("peer left")

func (c *udpClientConnection) Read(buf []byte) (int, error) {
	d := c.readTimeout
	if d == 0 {
		d = 3 * time.Second
	}
	select {
	case pkt := <-c.dataCh:
		if bytes.Equal(pkt, []byte("bye")) {
			return 0, errPeerLeft
		}
		n := copy(buf, pkt)
		return n, nil
	case <-time.After(d):
		return 0, errors.New("udpClient.Read timed out")
	}
}

func (c *udpClientConnection) Write(buf []byte) (int, error) {
	return c.conn.WriteTo(buf, c.addr)
}

func (s *UDPServer) route() {
	clients := map[netip.AddrPort]*udpClientConnection{}
	for pkt := range s.routerChan {
		client, ok := clients[pkt.addr.AddrPort()]
		if !ok {
			ir, err := s.buildInterceptorRegistry()
			if err != nil {
				log.Printf("failed to register interceptors for new receiver: %v", err)
				continue
			}
			i, err := ir.Build(strconv.Itoa(0))
			if err != nil {
				log.Printf("failed to build interceptors for new receiver: %v", err)
				continue
			}
			sink, err := s.mediaFactory.Create()
			if err != nil {
				log.Printf("failed to create media sink for new receiver: %v", err)
				continue
			}
			client = &udpClientConnection{
				conn:        pkt.conn,
				addr:        pkt.addr,
				dataCh:      make(chan []byte, 1024),
				readTimeout: 0,
			}
			receiver := newReceiver(s.mtu, demultiplexerFunc(func(pkt []byte) (uint64, []byte, error) {
				return 0, pkt, nil
			}))
			receiver.addIncomingFlow(0, i, sink, []io.ReadWriter{client})
			rtcpFlow := transport.NewRTCPFlow()
			rtcpFlow.Bind(client)
			i.BindRTCPWriter(rtcpFlow)
			go func() {
				defer func() {
					if err := i.Close(); err != nil {
						log.Printf("failed to close interceptor: %v", err)
					}
					if err := sink.Stop(); err != nil {
						log.Printf("pipeline.Stop error: %v", err)
					}
				}()
				if err := receiver.receive(); err != nil {
					if err == errPeerLeft {
						return
					}
					log.Printf("receive error: %v", err)
				}
			}()
			go func() {
				if err := sink.Play(); err != nil {
					log.Printf("pipeline.Play error: %v", err)
				}
			}()
			clients[pkt.addr.AddrPort()] = client
		}
		select {
		case client.dataCh <- pkt.buf:
		default:
			log.Printf("client buffer full, dropping message (%v)", client.addr)
		}
	}
}
