package rtc

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/twcc"
	"github.com/pion/rtp"
)

const transportCCURI = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"

type Sender struct {
	session quic.Session
	media   io.Reader
}

func NewSender(media io.Reader, addr string) (*Sender, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	session, err := quic.DialAddr(addr, tlsConf, &quic.Config{EnableDatagrams: true})
	if err != nil {
		return nil, err
	}

	return &Sender{
		session: session,
		media:   media,
	}, nil
}

func (s *Sender) Run(ctx context.Context) (err error) {
	buf := make([]byte, 1200)

	defer func() {
		log.Println("closing sender")
		err1 := s.session.CloseWithError(0, "eos")
		if err != nil {
			return
		}
		err = err1
	}()

	rtpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTPFormatter(rtpFormat),
		packetdump.RTPWriter(io.Discard),
	)

	rtcpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(io.Discard),
	)

	//var tx *scream.SenderInterceptorFactory
	//tx, err = scream.NewSenderInterceptor()
	//if err != nil {
	//	return
	//}

	headerExtension, err := twcc.NewHeaderExtensionInterceptor()
	if err != nil {
		return err
	}
	var gccFactory *gcc.InterceptorFactory
	gccFactory, err = gcc.NewInterceptor(gcc.InitialBitrate(10_000_000), gcc.SetPacer(gcc.NewLeakyBucketPacer(10_000_000)))
	if err != nil {
		return
	}

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				stats, err := gccFactory.GetStats("")
				if err != nil {
					panic(err)
				}
				log.Printf("%v\n", stats)
			}
		}
	}()

	ir := interceptor.Registry{}
	ir.Add(rtpDumperInterceptor)
	ir.Add(rtcpDumperInterceptor)
	ir.Add(gccFactory)
	ir.Add(headerExtension)
	//ir.Add(tx)

	var chain interceptor.Interceptor
	chain, err = ir.Build("")
	if err != nil {
		return
	}

	streamWriter := chain.BindLocalStream(&interceptor.StreamInfo{
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
	}, interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		headerBuf, err1 := header.Marshal()
		if err != nil {
			return 0, err1
		}

		return len(headerBuf) + len(payload), s.session.SendMessage(append(headerBuf, payload...), nil, nil)
		//return len(headerBuf) + len(payload), s.session.SendMessage(append(headerBuf, payload...))
	}))

	rtcpReader := chain.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	// TODO: Move to separate function
	go func() {
		for {
			buf, err := s.session.ReceiveMessage()
			if err != nil {
				// TODO
				panic(err)
			}

			if _, _, err = rtcpReader.Read(buf, nil); err != nil {
				// TODO
				panic(err)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			//var n int
			_, err = s.media.Read(buf)
			if err != nil {
				return
			}
			//log.Printf("%v bytes read from pipeline\n", n)
			var pkt rtp.Packet
			err = pkt.Unmarshal(buf)
			if err != nil {
				return
			}
			_, err = streamWriter.Write(&pkt.Header, pkt.Payload, nil)
			if err != nil {
				return
			}

			//log.Printf("%v bytes written to connection\n", n)
		}
	}
}
