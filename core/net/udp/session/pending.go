package session

import (
	"sync/atomic"

	"github.com/we-apranik/apranik/core/stats"
)

// How many datagrams a session counts for itself before it adds them to the
// shared counters. PROVISIONAL: 64.
const flushEveryDatagrams = 64

// What a session has counted for itself and not yet added to the counters it
// shares with other sessions. Every datagram of every session adding to the
// same inbound, user and outbound made the cores fight over those few cache
// lines; counting here, in memory only this session touches, and adding the
// total every flushEveryDatagrams datagrams (and when the session is
// flushed or ends) takes that contention away. A reader sees a session's
// numbers lag by at most that many datagrams, until the next flush.
type pendingDatagramCounts struct {
	uplinkBytes     atomic.Uint64
	uplinkPackets   atomic.Uint64
	downlinkBytes   atomic.Uint64
	downlinkPackets atomic.Uint64
}

// addUplink returns how many datagrams are pending now.
func (p *pendingDatagramCounts) addUplink(size int) uint64 {
	p.uplinkBytes.Add(uint64(size))
	return p.uplinkPackets.Add(1)
}

func (p *pendingDatagramCounts) addDownlink(size int) uint64 {
	p.downlinkBytes.Add(uint64(size))
	return p.downlinkPackets.Add(1)
}

// flushInto adds what is pending to every counters entry and starts again from
// zero. Two flushes at once split what is pending between them, and nothing is
// lost or counted twice.
func (p *pendingDatagramCounts) flushInto(counters []*stats.Counters) {
	uplinkBytes, uplinkPackets := p.uplinkBytes.Swap(0), p.uplinkPackets.Swap(0)
	downlinkBytes, downlinkPackets := p.downlinkBytes.Swap(0), p.downlinkPackets.Swap(0)
	if uplinkPackets == 0 && downlinkPackets == 0 && uplinkBytes == 0 && downlinkBytes == 0 {
		return
	}
	for _, counted := range counters {
		counted.AddUplinkDatagrams(uplinkBytes, uplinkPackets)
		counted.AddDownlinkDatagrams(downlinkBytes, downlinkPackets)
	}
}
