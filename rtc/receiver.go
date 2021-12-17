package rtc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
)

type MediaSink interface {
	io.WriteCloser
}

type receiveFlow struct {
	media  io.WriteCloser
	reader interceptor.RTPReader
}

type Receiver struct {
	session     quic.Session
	flows       map[uint64]*receiveFlow
	interceptor interceptor.Interceptor
	wg          sync.WaitGroup
}

type ReceiverConfig struct {
	Dump     bool
	RTPDump  io.Writer
	RTCPDump io.Writer
	RFC8888  bool
	TWCC     bool
}

func GstreamerReceiverFactory(c ReceiverConfig) (ReceiverFactory, error) {
	ir := interceptor.Registry{}
	if c.Dump {
		registerRTPReceiverDumper(&ir, c.RTPDump, c.RTCPDump)
	}
	if c.RFC8888 {
		registerRFC8888(&ir)
	}
	if c.TWCC {
		registerTWCC(&ir)
	}
	interceptor, err := ir.Build("")
	if err != nil {
		return nil, err
	}
	return func(session quic.Session, sinkFactory MediaSinkFactory) (*Receiver, error) {
		sink, err := sinkFactory()
		if err != nil {
			return nil, err
		}
		receiver, err := newReceiver(session, interceptor)
		if err != nil {
			return nil, err
		}
		receiver.setFlow(0, sink)
		return receiver, nil
	}, nil
}

func newReceiver(session quic.Session, interceptor interceptor.Interceptor) (*Receiver, error) {
	return &Receiver{
		session:     session,
		flows:       map[uint64]*receiveFlow{},
		interceptor: interceptor,
		wg:          sync.WaitGroup{},
	}, nil
}

func (r *Receiver) setFlow(id uint64, pipeline io.WriteCloser) {
	streamReader := r.interceptor.BindRemoteStream(&interceptor.StreamInfo{
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
	}, interceptor.RTPReaderFunc(func(b []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
		n, err := pipeline.Write(b)
		if err != nil {
			return n, nil, err
		}
		return len(b), nil, nil
	}))

	r.flows[id] = &receiveFlow{
		media:  pipeline,
		reader: streamReader,
	}
}

func (r *Receiver) run(ctx context.Context) (err error) {
	r.wg.Add(1)
	defer r.wg.Done()
	defer func() {
		log.Println("closing receiver")
		err1 := r.session.CloseWithError(0, "eos")
		if err != nil {
			return
		}
		err = err1
	}()

	_ = r.interceptor.BindRTCPWriter(interceptor.RTCPWriterFunc(r.rtcpWriter))

	defer r.interceptor.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			buf, err := r.session.ReceiveMessage()
			if err != nil {
				return err
			}
			//log.Printf("%v bytes read from connection\n", len(buf))

			id, err := quicvarint.Read(bytes.NewReader(buf))
			if err != nil {
				log.Printf("failed to read flow ID: %v, dropping datagram\n", err)
				continue
			}
			n := quicvarint.Len(id)
			packet := buf[n:]
			flow, ok := r.flows[id]
			if !ok {
				log.Printf("got datagram with unknown flow ID (%v), dropping datagram\n", id)
				continue
			}
			//log.Printf("writing %v bytes to flow %v\n", len(packet), id)
			if _, _, err := flow.reader.Read(packet, nil); err != nil {
				panic(err)
			}
			//log.Printf("%v bytes written to pipeline\n", len(buf))
		}
	}
}

func (r *Receiver) rtcpWriter(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
	buf, err := rtcp.Marshal(pkts)
	if err != nil {
		return 0, err
	}
	//return len(buf), r.session.SendMessage(buf, nil, nil)
	return len(buf), r.session.SendMessage(buf)
}

func (r *Receiver) Close() error {
	defer fmt.Println("Receiver closed")
	defer r.wg.Wait()
	for _, flow := range r.flows {
		if err := flow.media.Close(); err != nil {
			return err
		}
	}
	return nil
}
