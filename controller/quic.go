package controller

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/logging"
	"github.com/lucas-clemente/quic-go/qlog"
)

func connectQUIC(
	addr string,
	cc CongestionControlAlgorithm,
	metricsTracer logging.Tracer,
	qlogDirectoryName string,
	sslKeyLogFileName string,
) (quic.Connection, error) {
	qlogWriter, err := getQLOGTracer(qlogDirectoryName)
	if err != nil {
		return nil, err
	}
	keyLogger, err := getKeyLogger(sslKeyLogFileName)
	if err != nil {
		return nil, err
	}
	tlsConf := &tls.Config{
		KeyLogWriter:       keyLogger,
		InsecureSkipVerify: true,
		NextProtos:         []string{"rtq"},
	}
	tracers := []logging.Tracer{metricsTracer}
	if qlogWriter != nil {
		tracers = append(tracers, qlogWriter)
	}
	tracer := logging.NewMultiplexedTracer(tracers...)
	quicConf := &quic.Config{
		EnableDatagrams:      true,
		HandshakeIdleTimeout: 15 * time.Second,
		Tracer:               tracer,
		DisableCC:            cc != Reno,
	}
	session, err := quic.DialAddr(addr, tlsConf, quicConf)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func getQLOGTracer(path string) (logging.Tracer, error) {
	if len(path) == 0 {
		return nil, nil
	}
	if path == "stdout" {
		return qlog.NewTracer(func(p logging.Perspective, connectionID []byte) io.WriteCloser {
			return nopCloser{os.Stdout}
		}), nil
	}
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(path, 0o666); err != nil {
				return nil, fmt.Errorf("failed to create qlog dir %s: %v", path, err)
			}
		} else {
			return nil, err
		}
	}
	return qlog.NewTracer(func(p logging.Perspective, connectionID []byte) io.WriteCloser {
		file := fmt.Sprintf("%s/%x_%v.qlog", strings.TrimRight(path, "/"), connectionID, p)
		w, err := os.Create(file)
		if err != nil {
			log.Printf("failed to create qlog file %s: %v", path, err)
			return nil
		}
		log.Printf("created qlog file: %s\n", path)
		return w
	}), nil
}

func getKeyLogger(keyLogFile string) (io.Writer, error) {
	if len(keyLogFile) == 0 {
		return nil, nil
	}
	keyLogger, err := os.OpenFile(keyLogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	return keyLogger, nil
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }
