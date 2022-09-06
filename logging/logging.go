package logging

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/lucas-clemente/quic-go/logging"
	"github.com/lucas-clemente/quic-go/qlog"
)

func GetLogFile(file string) (io.WriteCloser, error) {
	if len(file) == 0 {
		return nopCloser{io.Discard}, nil
	}
	if file == "stdout" {
		return nopCloser{os.Stdout}, nil
	}
	fd, err := os.Create(file)
	if err != nil {
		return nil, err
	}
	bufwriter := bufio.NewWriterSize(fd, 4096)

	return &fileCloser{
		f:   fd,
		buf: bufwriter,
	}, nil
}

func GetQLOGTracer(path string) (logging.Tracer, error) {
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

func GetKeyLogger(keyLogFile string) (io.Writer, error) {
	if len(keyLogFile) == 0 {
		return nil, nil
	}
	keyLogger, err := os.OpenFile(keyLogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	return keyLogger, nil
}

type fileCloser struct {
	f   *os.File
	buf *bufio.Writer
}

func (f *fileCloser) Write(buf []byte) (int, error) {
	return f.f.Write(buf)
}

func (f *fileCloser) Close() error {
	if err := f.buf.Flush(); err != nil {
		log.Printf("failed to flush: %v\n", err)
	}
	return f.f.Close()
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }
