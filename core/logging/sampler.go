package logging

import (
	"sync/atomic"
	"time"
)

// Sampler lets one event through per interval and counts the rest, so a flood
// a remote peer can cause becomes one log line per interval that says how many
// events it stands for. Safe for concurrent use.
type Sampler struct {
	interval            time.Duration
	nextAllowedUnixNano atomic.Int64
	suppressed          atomic.Int64
}

func NewSampler(interval time.Duration) *Sampler {
	return &Sampler{interval: interval}
}

// Allow reports whether to log this event, and if so how many events were
// held back since the last one that was logged.
func (s *Sampler) Allow() (bool, int64) {
	now := time.Now().UnixNano()
	nextAllowed := s.nextAllowedUnixNano.Load()
	if now >= nextAllowed && s.nextAllowedUnixNano.CompareAndSwap(nextAllowed, now+int64(s.interval)) {
		return true, s.suppressed.Swap(0)
	}
	s.suppressed.Add(1)
	return false, 0
}
