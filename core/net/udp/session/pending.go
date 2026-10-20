package session

import (
	"sync/atomic"

	"github.com/we-apranik/apranik/core/stats"
)

// PROVISIONAL: 64.
const flushEveryDatagrams = 64

type pendingDatagramCounts struct {
	uplinkBytes     atomic.Uint64
	uplinkPackets   atomic.Uint64
	downlinkBytes   atomic.Uint64
	downlinkPackets atomic.Uint64
}

func (p *pendingDatagramCounts) addUplink(size int) uint64 {
	p.uplinkBytes.Add(uint64(size))
	return p.uplinkPackets.Add(1)
}

func (p *pendingDatagramCounts) addDownlink(size int) uint64 {
	p.downlinkBytes.Add(uint64(size))
	return p.downlinkPackets.Add(1)
}

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
