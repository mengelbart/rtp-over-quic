package rtp

import (
	"context"
	"log"

	"github.com/pion/interceptor"
)

type RTCPFeedback struct {
	Buffer     []byte
	Attributes interceptor.Attributes
}

func ReadRTCP(ctx context.Context, rtcpReader interceptor.RTCPReader, rtcpChan <-chan RTCPFeedback) {
	for {
		select {
		case report := <-rtcpChan:
			_, _, err := rtcpReader.Read(report.Buffer, report.Attributes)
			if err != nil {
				log.Printf("RTCP reader returned error: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
