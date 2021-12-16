package rtc

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/lucas-clemente/quic-go"
	gstsink "github.com/mengelbart/gst-go/gstreamer-sink"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/twcc"
	"github.com/pion/rtcp"
)

type Receiver struct {
	session quic.Session
	media   io.WriteCloser
}

func CreateGstreamerReceiver(session quic.Session) (*Receiver, error) {
	dstPipeline, err := gstsink.NewPipeline("vp8", "autovideosink")
	if err != nil {
		return nil, err
	}
	dstPipeline.Start()
	return newReceiver(dstPipeline, session)
}

func newReceiver(media io.WriteCloser, session quic.Session) (*Receiver, error) {
	return &Receiver{
		session: session,
		media:   media,
	}, nil
}

func (r *Receiver) run(ctx context.Context) (err error) {
	defer func() {
		log.Println("closing receiver")
		err1 := r.session.CloseWithError(0, "eos")
		if err != nil {
			return
		}
		err = err1
	}()

	//var rx *scream.ReceiverInterceptorFactory
	//rx, err = scream.NewReceiverInterceptor()
	//if err != nil {
	//	return
	//}

	rtcpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(io.Discard),
	)
	if err != nil {
		return
	}

	rtpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTPFormatter(rtpFormat),
		packetdump.RTPWriter(os.Stdout),
	)
	if err != nil {
		return
	}

	var fbFactory *twcc.SenderInterceptorFactory
	fbFactory, err = twcc.NewSenderInterceptor()
	if err != nil {
		return
	}

	ir := interceptor.Registry{}
	ir.Add(rtcpDumperInterceptor)
	ir.Add(rtpDumperInterceptor)
	ir.Add(fbFactory)
	//ir.Add(rx)

	var chain interceptor.Interceptor
	chain, err = ir.Build("")
	if err != nil {
		return
	}

	streamReader := chain.BindRemoteStream(&interceptor.StreamInfo{
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
		n, err := r.media.Write(b)
		if err != nil {
			return n, nil, err
		}
		return len(b), nil, nil
	}))

	_ = chain.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
		buf, err := rtcp.Marshal(pkts)
		if err != nil {
			return 0, err
		}
		return len(buf), r.session.SendMessage(buf, nil, nil)
		//return len(buf), r.session.SendMessage(buf)
	}))

	defer chain.Close()

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
			//log.Printf("%v bytes read from connection\n", len(buf))

			if _, _, err := streamReader.Read(buf, nil); err != nil {
				panic(err)
			}
			//log.Printf("%v bytes written to pipeline\n", len(buf))
		}
	}
}

func (r *Receiver) Close() error {
	return r.media.Close()
}
