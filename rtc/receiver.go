package rtc

import (
	"context"
	"io"
	"log"
	"sync"

	"github.com/lucas-clemente/quic-go"
	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
)

type Receiver struct {
	session     quic.Session
	media       io.WriteCloser
	interceptor interceptor.Interceptor
	wg          sync.WaitGroup
}

type ReceiverConfig struct {
	Dump    bool
	RFC8888 bool
	TWCC    bool
}

func GstreamerReceiverFactory(c ReceiverConfig) ReceiverFactory {
	ir := interceptor.Registry{}
	if c.Dump {
		registerRTPReceiverDumper(&ir)
	}
	if c.RFC8888 {
		registerRFC8888(&ir)
	}
	if c.TWCC {
		registerTWCC(&ir)
	}
	return func(session quic.Session) (*Receiver, error) {
		dstPipeline, err := gstsink.NewPipeline("h264", "autovideosink")
		if err != nil {
			return nil, err
		}
		dstPipeline.Start()
		interceptor, err := ir.Build("")
		if err != nil {
			return nil, err
		}
		return newReceiver(dstPipeline, session, interceptor)
	}
}

func newReceiver(media io.WriteCloser, session quic.Session, interceptor interceptor.Interceptor) (*Receiver, error) {
	return &Receiver{
		session:     session,
		media:       media,
		interceptor: interceptor,
	}, nil
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
	}, interceptor.RTPReaderFunc(r.rtpReader))

	_ = r.interceptor.BindRTCPWriter(interceptor.RTCPWriterFunc(r.rtcpWriter))

	defer r.interceptor.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var buf []byte
			buf, err = r.session.ReceiveMessage()
			if err != nil {
				return
			}
			log.Printf("%v bytes read from connection\n", len(buf))

			if _, _, err := streamReader.Read(buf, nil); err != nil {
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

func (r *Receiver) rtpReader(b []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
	n, err := r.media.Write(b)
	if err != nil {
		return n, nil, err
	}
	return len(b), nil, nil
}

func (r *Receiver) Close() error {
	defer r.wg.Wait()
	return r.media.Close()
}
