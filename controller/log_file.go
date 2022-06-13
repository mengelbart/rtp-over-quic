package controller

import (
	"bufio"
	"io"
	"log"
	"os"
)

func getLogFile(file string) (io.WriteCloser, error) {
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
