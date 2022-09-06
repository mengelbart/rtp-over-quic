package quic

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"time"

	"github.com/lucas-clemente/quic-go"
	quiclogging "github.com/lucas-clemente/quic-go/logging"
	"github.com/lucas-clemente/quic-go/quicvarint"
	"github.com/mengelbart/rtp-over-quic/cc"
	"github.com/mengelbart/rtp-over-quic/logging"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

const rtpOverQUICALPN = "rtp-mux-quic"

type rtcpFeedback struct {
	buf        []byte
	attributes interceptor.Attributes
}

type SenderOption func(*SenderConfig) error

func RemoteAddress(addr string) SenderOption {
	return func(sc *SenderConfig) error {
		sc.remoteAddr = addr
		return nil
	}
}

func SetQLOGDirName(dir string) SenderOption {
	return func(sc *SenderConfig) error {
		sc.qlogDirectoryName = dir
		return nil
	}
}

func SetSSLKeyLogFileName(file string) SenderOption {
	return func(sc *SenderConfig) error {
		sc.sslKeyLogFileName = file
		return nil
	}
}

func SetQUICCongestionControlAlgorithm(algorithm cc.Algorithm) SenderOption {
	return func(sc *SenderConfig) error {
		sc.cc = algorithm
		return nil
	}
}

type SenderConfig struct {
	cc                cc.Algorithm
	remoteAddr        string
	qlogDirectoryName string
	sslKeyLogFileName string
}

type Sender struct {
	*SenderConfig

	conn          quic.Connection
	metricsTracer quiclogging.Tracer
	interceptor   interceptor.Interceptor
	rtcpChan      chan rtcpFeedback
}

func NewSender(i interceptor.Interceptor, opts ...SenderOption) (*Sender, error) {
	s := &Sender{
		SenderConfig:  &SenderConfig{},
		conn:          nil,
		metricsTracer: nil,
		interceptor:   i,
		rtcpChan:      make(chan rtcpFeedback),
	}
	for _, opt := range opts {
		if err := opt(s.SenderConfig); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Sender) Connect(ctx context.Context) error {
	qlogWriter, err := logging.GetQLOGTracer(s.qlogDirectoryName)
	if err != nil {
		return err
	}
	keyLogger, err := logging.GetKeyLogger(s.sslKeyLogFileName)
	if err != nil {
		return err
	}
	tlsConf := &tls.Config{
		KeyLogWriter:       keyLogger,
		InsecureSkipVerify: true,
		NextProtos:         []string{rtpOverQUICALPN},
	}
	s.metricsTracer = NewTracer()
	tracers := []quiclogging.Tracer{s.metricsTracer}
	if qlogWriter != nil {
		tracers = append(tracers, qlogWriter)
	}
	tracer := quiclogging.NewMultiplexedTracer(tracers...)
	quicConf := &quic.Config{
		EnableDatagrams:       true,
		HandshakeIdleTimeout:  15 * time.Second,
		Tracer:                tracer,
		DisableCC:             s.cc != cc.Reno,
		MaxIncomingStreams:    1 << 60,
		MaxIncomingUniStreams: 1 << 60,
	}
	conn, err := quic.DialAddrContext(ctx, s.remoteAddr, tlsConf, quicConf)
	if err != nil {
		return err
	}
	s.conn = conn

	rtcpReader := s.interceptor.BindRTCPReader(interceptor.RTCPReaderFunc(
		func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
			return len(b), a, nil
		}),
	)
	go s.readRTCP(ctx, rtcpReader)
	go s.readFromNetwork(ctx)

	return nil
}

func (s *Sender) readFromNetwork(ctx context.Context) {
	for {
		buf, err := s.conn.ReceiveMessage()
		if err != nil {
			if e, ok := err.(*quic.ApplicationError); ok && e.ErrorCode == 0 {
				log.Printf("QUIC received application error, exiting reader routine: %v", err)
				return
			}
			log.Printf("failed to receive QUIC datagram: %v", err)
			continue
		}
		// TODO: If multiple RTCP flows are required, demultiplex on id here
		id, err := quicvarint.Read(bytes.NewReader(buf))
		if err != nil {
			log.Printf("failed to read flow ID: %v, dropping datagram", err)
			continue
		}
		s.rtcpChan <- rtcpFeedback{
			buf:        buf[quicvarint.Len(id):],
			attributes: nil,
		}
	}
}

func (s *Sender) readRTCP(ctx context.Context, reader interceptor.RTCPReader) {
	for {
		select {
		case report := <-s.rtcpChan:
			_, _, err := reader.Read(report.buf, report.attributes)
			if err != nil {
				log.Printf("RTCP reader returned error: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Sender) writeDgram(buf []byte) (int, error) {
	return len(buf), s.conn.SendMessage(buf, nil, nil)
}

func (s *Sender) writeWithAckLossCallback(buf []byte, cb func(bool)) (int, error) {
	return len(buf), s.conn.SendMessage(buf, nil, cb)
}

func (s *Sender) NewMediaStream() interceptor.RTPWriter {
	id := uint64(0)
	var idBuffer bytes.Buffer
	idWriter := quicvarint.NewWriter(&idBuffer)
	quicvarint.Write(idWriter, id)
	ms := &MediaStream{
		sender:   s,
		flowID:   id,
		varIntID: idBuffer.Bytes(),
		writer:   nil,
	}
	writer := s.interceptor.BindLocalStream(&interceptor.StreamInfo{}, interceptor.RTPWriterFunc(
		func(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
			headerBuf, err := header.Marshal()
			if err != nil {
				return 0, err
			}
			pl := append(ms.varIntID, headerBuf...)
			pl = append(pl, payload...)
			return s.writeDgram(pl)
		},
	))
	ms.writer = writer
	return ms
}

type MediaStream struct {
	sender   *Sender
	flowID   uint64
	varIntID []byte
	writer   interceptor.RTPWriter
}

func (s *MediaStream) Write(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
	return s.writer.Write(header, payload, attributes)
}

// Setup a bare-bones TLS config for the server
func generateTLSConfig(keyLogWriter io.Writer) *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		KeyLogWriter: keyLogWriter,
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{rtpOverQUICALPN},
	}
}
