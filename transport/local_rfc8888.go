package transport

import (
	"sort"
	"sync"
	"time"

	screamcgo "github.com/mengelbart/scream-go"
)

type RTTStats struct {
	MinRTT      time.Duration
	SmoothedRTT time.Duration
	RTTVar      time.Duration
	LatestRTT   time.Duration
}

type Metricer interface {
	Metrics() RTTStats
}

type Feedback struct {
	Buf       []byte
	Timestamp time.Time
}

type ackedPkt struct {
	sentTS time.Time
	ssrc   uint32
	size   int
	seqNr  uint16
}

func getNTPT0() float64 {
	now := time.Now()
	secs := now.Unix()
	usecs := now.UnixMicro() - secs*1e6
	return (float64(secs) + float64(usecs)*1e-6) - 1e-3
}

func getTimeBetweenNTP(t0 float64, tx time.Time) uint64 {
	secs := tx.Unix()
	usecs := tx.UnixMicro() - secs*1e6
	tt := (float64(secs) + float64(usecs)*1e-6) - t0
	ntp64 := uint64(tt * 65536.0)
	ntp := 0xFFFFFFFF & ntp64
	return ntp
}

type localRFC8888Generator struct {
	rx        *screamcgo.Rx
	m         Metricer
	reportCB  func(Feedback)
	ackedPkts chan ackedPkt
	t0        float64
	close     chan struct{}
	wg        sync.WaitGroup
}

func newLocalRFC8888Generator(ssrc uint32, m Metricer, reportCB func(Feedback)) *localRFC8888Generator {
	return &localRFC8888Generator{
		rx:        screamcgo.NewRx(0),
		m:         m,
		reportCB:  reportCB,
		ackedPkts: make(chan ackedPkt),
		t0:        getNTPT0(),
		close:     make(chan struct{}),
		wg:        sync.WaitGroup{},
	}
}

func (f *localRFC8888Generator) ntpTime(t time.Time) uint64 {
	return getTimeBetweenNTP(f.t0, t)
}

func (f *localRFC8888Generator) ack(pkt ackedPkt) {
	f.ackedPkts <- pkt
}

func (f *localRFC8888Generator) Run() {
	f.wg.Add(1)
	defer f.wg.Done()

	t := time.NewTicker(10 * time.Millisecond)
	var buf []ackedPkt
	for {
		select {
		case pkt := <-f.ackedPkts:
			buf = append(buf, pkt)

		case <-t.C:
			if len(buf) == 0 {
				continue
			}
			sort.Slice(buf, func(i, j int) bool {
				return buf[i].seqNr < buf[j].seqNr
			})

			metrics := f.m.Metrics()

			var lastTS uint64
			for _, pkt := range buf {
				sent := f.ntpTime(pkt.sentTS)
				rttNTP := metrics.LatestRTT.Seconds() * 65536
				lastTS = sent + uint64(rttNTP)/2
				f.rx.Receive(lastTS, pkt.ssrc, pkt.size, pkt.seqNr, 0)
			}
			buf = []ackedPkt{}

			if ok, fb := f.rx.CreateStandardizedFeedback(lastTS, true); ok {
				f.reportCB(Feedback{
					Buf:       fb,
					Timestamp: time.Now(),
				})
			}

		case <-f.close:
			return
		}
	}
}

func (f *localRFC8888Generator) isClosed() bool {
	select {
	case <-f.close:
		return true
	default:
		return false
	}
}

func (f *localRFC8888Generator) Close() error {
	defer f.wg.Wait()
	if !f.isClosed() {
		close(f.close)
	}
	return nil
}
