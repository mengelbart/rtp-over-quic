package rtc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	quic "github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/gcc/pkg/gcc"
	"github.com/pion/interceptor/scream/pkg/scream"
	"github.com/pion/rtp"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type SenderFactory func(MediaSource) (*Sender, error)

type MediaSource interface {
	io.Reader
	SetBitRate(uint)
}

type sendFlow struct {
	media  io.Reader
	writer interceptor.RTPWriter
}

type Sender struct {
	session     quic.Session
	flows       map[uint64]*sendFlow
	interceptor interceptor.Interceptor
	done        chan struct{}
	wg          sync.WaitGroup
}

type SenderConfig struct {
	Dump     bool
	RTPDump  io.Writer
	RTCPDump io.Writer
	SCReAM   bool
	GCC      bool
}

type rateController struct {
	pipelines []MediaSource
}

func (c *rateController) addPipeline(p MediaSource) {
	c.pipelines = append(c.pipelines, p)
}

func (c *rateController) screamLoopFactory(ctx context.Context) scream.NewPeerConnectionCallback {
	return func(_ string, bwe scream.BandwidthEstimator) {
		go func() {
			ticker := time.NewTicker(200 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					target, err := bwe.GetTargetBitrate(0)
					if err != nil {
						log.Printf("failed to get target bitrate: %v\n", err)
					}
					if target < 0 {
						log.Printf("got negative target bitrate: %v\n", target)
					}
					share := target / len(c.pipelines)
					for _, p := range c.pipelines {
						p.SetBitRate(uint(share))
						fmt.Printf("new bitrate: %v\n", target)
					}
				}
			}
		}()
	}
}

func (c *rateController) gccLoopFactory(ctx context.Context) gcc.NewPeerConnectionCallback {
	return func(_ string, bwe gcc.BandwidthEstimator) {
		go func() {
			ticker := time.NewTicker(200 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					target := bwe.GetTargetBitrate()
					if target < 0 {
						log.Printf("got negative target bitrate: %v\n", target)
						continue
					}
					fmt.Printf("new bitrate: %v\n", target)
					if len(c.pipelines) == 0 {
						continue
					}
					share := target / len(c.pipelines)
					for _, p := range c.pipelines {
						p.SetBitRate(uint(share))
					}
				}
			}
		}()
	}
}

func GstreamerSenderFactory(ctx context.Context, c SenderConfig, session quic.Session) (SenderFactory, error) {
	ir := interceptor.Registry{}
	if c.Dump {
		if err := registerRTPSenderDumper(&ir, c.RTPDump, c.RTCPDump); err != nil {
			return nil, err
		}
	}
	var rc rateController
	if c.SCReAM {
		if err := registerSCReAM(&ir, rc.screamLoopFactory(ctx)); err != nil {
			return nil, err
		}
	}
	if c.GCC {
		if err := registerGCC(&ir, rc.gccLoopFactory(ctx)); err != nil {
			return nil, err
		}
		if err := registerTWCCHeaderExtension(&ir); err != nil {
			return nil, err
		}
	}
	interceptor, err := ir.Build("")
	if err != nil {
		return nil, err
	}
	return func(src MediaSource) (*Sender, error) {
		rc.addPipeline(src)

		sender, err := newSender(session, interceptor)
		if err != nil {
			return nil, err
		}
		sender.setFlow(0, src)
		return sender, nil
	}, nil
}

func newSender(session quic.Session, interceptor interceptor.Interceptor) (*Sender, error) {
	return &Sender{
		session:     session,
		flows:       map[uint64]*sendFlow{},
		interceptor: interceptor,
		done:        make(chan struct{}),
		wg:          sync.WaitGroup{},
	}, nil
}

func (s *Sender) setFlow(id uint64, pipeline io.Reader) {
	streamWriter := s.interceptor.BindLocalStream(&interceptor.StreamInfo{
		ID:                  "",
		Attributes:          map[interface{}]interface{}{},
		SSRC:                0,
		PayloadType:         0,
		RTPHeaderExtensions: []interceptor.RTPHeaderExtension{{URI: transportCCURI, ID: 1}},
		MimeType:            "",
		ClockRate:           0,
		Channels:            0,
		SDPFmtpLine:         "",
		RTCPFeedback:        []interceptor.RTCPFeedback{{Type: "ack", Parameter: "ccfb"}},
	}, s.getRTPWriter(id))

	s.flows[id] = &sendFlow{
		media:  pipeline,
		writer: streamWriter,
	}
}

func (s *Sender) Run() (err error) {
	s.wg.Add(1)
	defer s.wg.Done()

	rtcpReader := s.interceptor.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	go s.readRTCP(rtcpReader)

	buf := make([]byte, 1200)
	for {
		select {
		case <-s.done:
			return nil
		default:
			for _, flow := range s.flows {
				n, err := flow.media.Read(buf)
				if err != nil {
					return err
				}
				//log.Printf("%v bytes read from pipeline\n", n)
				var pkt rtp.Packet
				err = pkt.Unmarshal(buf[:n])
				if err != nil {
					return err
				}
				_, err = flow.writer.Write(&pkt.Header, pkt.Payload, nil)
				if err != nil {
					if errors.Is(errConnectionClosed, err) {
						return nil
					}
					return err
				}
			}
			//log.Printf("%v bytes written to connection\n", n)
		}
	}
}

func (s *Sender) readRTCP(rtcpReader interceptor.RTCPReader) {
	for {
		buf, err := s.session.ReceiveMessage()
		if err != nil {
			if qerr, ok := err.(*quic.ApplicationError); ok && qerr.ErrorCode == 0 {
				log.Printf("connection closed, exiting")
				return
			}
			log.Printf("session.ReceiveMessage returned error: %v, exiting RTCP reader\n", err)
			return
		}

		if _, _, err = rtcpReader.Read(buf, nil); err != nil {
			log.Printf("rtcpReader.Read returned error: %v, exiting RTCP reader\n", err)
			return
		}
	}
}

var errConnectionClosed = errors.New("connection closed")

func (s *Sender) getRTPWriter(id uint64) interceptor.RTPWriter {
	var buf bytes.Buffer
	idWriter := quicvarint.NewWriter(&buf)
	quicvarint.Write(idWriter, id)
	idBytes := buf.Bytes()
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		if s.isClosed() {
			return 0, errConnectionClosed
		}
		headerBuf, err := header.Marshal()
		if err != nil {
			log.Printf("failed to marshal header: %v\n", err)
			return 0, err
		}

		//if err := s.session.SendMessage(append(headerBuf, payload...), nil, nil); err != nil {
		packetBuffer := append(headerBuf, payload...)
		dgramBuffer := append(idBytes, packetBuffer...)
		if err := s.session.SendMessage(dgramBuffer); err != nil {
			s.close()
			if qerr, ok := err.(*quic.ApplicationError); ok && qerr.ErrorCode == 0 {
				log.Printf("connection closed by remote")
				return 0, errConnectionClosed
			}
			log.Printf("failed to sendMessage: %v, closing\n", err)
			return 0, err
		}
		//log.Printf("%v bytes written to connection\n", len(dgramBuffer))
		return len(packetBuffer), nil
	})
}

func (s *Sender) close() {
	if !s.isClosed() {
		close(s.done)
	}
}

func (s *Sender) isClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Sender) Close() error {
	for _, flow := range s.flows {
		go func() {
			if _, err := io.ReadAll(flow.media); err != nil {
				panic(err)
			}
		}()
	}
	s.close()
	s.wg.Wait()
	if err := s.interceptor.Close(); err != nil {
		return err
	}
	if err := s.session.CloseWithError(0, "eos"); err != nil {
		return err
	}
	return nil
}
