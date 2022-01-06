package rtc

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go/logging"
)

type RTTTracer struct {
	lock sync.Mutex

	MinRTT      time.Duration
	SmoothedRTT time.Duration
	RTTVar      time.Duration
	LatestRTT   time.Duration
}

func (q *RTTTracer) Metrics() RTTStats {
	q.lock.Lock()
	defer q.lock.Unlock()
	return RTTStats{
		MinRTT:      q.MinRTT,
		SmoothedRTT: q.SmoothedRTT,
		RTTVar:      q.RTTVar,
		LatestRTT:   q.LatestRTT,
	}
}

func (q *RTTTracer) updateMinRTT(minrtt time.Duration) {
	q.lock.Lock()
	defer q.lock.Unlock()
	q.MinRTT = minrtt
}

func (q *RTTTracer) updateSmoothedRTT(srtt time.Duration) {
	q.lock.Lock()
	defer q.lock.Unlock()
	q.SmoothedRTT = srtt
}

func (q *RTTTracer) updateRTTVar(rttvar time.Duration) {
	q.lock.Lock()
	defer q.lock.Unlock()
	q.RTTVar = rttvar
}

func (q *RTTTracer) updateLatestRTT(rttvar time.Duration) {
	q.lock.Lock()
	defer q.lock.Unlock()
	q.LatestRTT = rttvar
}

func NewTracer() *RTTTracer {
	return &RTTTracer{}
}

func (q *RTTTracer) TracerForConnection(ctx context.Context, p logging.Perspective, odcid logging.ConnectionID) logging.ConnectionTracer {
	return &ConnectionRTTTracer{
		t: q,
	}
}

func (q *RTTTracer) SentPacket(addr net.Addr, header *logging.Header, count logging.ByteCount, frames []logging.Frame) {
}

func (q *RTTTracer) DroppedPacket(addr net.Addr, packetType logging.PacketType, count logging.ByteCount, reason logging.PacketDropReason) {
}

type ConnectionRTTTracer struct {
	t *RTTTracer
}

func (c *ConnectionRTTTracer) SentPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, ack *logging.AckFrame, frames []logging.Frame) {
}

func (c *ConnectionRTTTracer) ReceivedPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, frames []logging.Frame) {
}

func (c *ConnectionRTTTracer) RestoredTransportParameters(parameters *logging.TransportParameters) {
}

func (c ConnectionRTTTracer) ReceivedVersionNegotiationPacket(header *logging.Header, numbers []logging.VersionNumber) {
}

func (c ConnectionRTTTracer) NegotiatedVersion(chosen logging.VersionNumber, clientVersions, serverVersions []logging.VersionNumber) {
}

func (c ConnectionRTTTracer) ReceivedRetry(header *logging.Header) {
}

func (c ConnectionRTTTracer) StartedConnection(local, remote net.Addr, srcConnID, destConnID logging.ConnectionID) {
}

func (c ConnectionRTTTracer) ClosedConnection(error) {
}

func (c ConnectionRTTTracer) SentTransportParameters(parameters *logging.TransportParameters) {
}

func (c ConnectionRTTTracer) ReceivedTransportParameters(parameters *logging.TransportParameters) {
}

func (c ConnectionRTTTracer) BufferedPacket(packetType logging.PacketType) {
}

func (c ConnectionRTTTracer) DroppedPacket(packetType logging.PacketType, count logging.ByteCount, reason logging.PacketDropReason) {
}

func (c *ConnectionRTTTracer) UpdatedMetrics(rttStats *logging.RTTStats, cwnd, bytesInFlight logging.ByteCount, packetsInFlight int) {
	min := rttStats.MinRTT()
	smoothed := rttStats.SmoothedRTT()
	rttVar := rttStats.MeanDeviation()
	latestRTT := rttStats.LatestRTT()
	if min != 0 {
		c.t.updateMinRTT(min)
	}
	if smoothed != 0 {
		c.t.updateSmoothedRTT(smoothed)
	}
	if rttVar != 0 {
		c.t.updateRTTVar(rttVar)
	}
	if latestRTT != 0 {
		c.t.updateLatestRTT(latestRTT)
	}
}

func (c ConnectionRTTTracer) AcknowledgedPacket(logging.EncryptionLevel, logging.PacketNumber) {
}

func (c ConnectionRTTTracer) LostPacket(level logging.EncryptionLevel, number logging.PacketNumber, reason logging.PacketLossReason) {
}

func (c ConnectionRTTTracer) UpdatedCongestionState(state logging.CongestionState) {
}

func (c ConnectionRTTTracer) UpdatedPTOCount(value uint32) {
}

func (c ConnectionRTTTracer) UpdatedKeyFromTLS(level logging.EncryptionLevel, perspective logging.Perspective) {
}

func (c ConnectionRTTTracer) UpdatedKey(generation logging.KeyPhase, remote bool) {
}

func (c ConnectionRTTTracer) DroppedEncryptionLevel(level logging.EncryptionLevel) {
}

func (c ConnectionRTTTracer) DroppedKey(generation logging.KeyPhase) {
}

func (c ConnectionRTTTracer) SetLossTimer(timerType logging.TimerType, level logging.EncryptionLevel, time time.Time) {
}

func (c ConnectionRTTTracer) LossTimerExpired(timerType logging.TimerType, level logging.EncryptionLevel) {
}

func (c ConnectionRTTTracer) LossTimerCanceled() {
}

func (c ConnectionRTTTracer) Close() {
}

func (c ConnectionRTTTracer) Debug(name, msg string) {
}
