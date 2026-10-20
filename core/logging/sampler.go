package logging

import (
	"sync/atomic"
	"time"
)

type Sampler struct {
	interval            time.Duration
	nextAllowedUnixNano atomic.Int64
	suppressed          atomic.Int64
}

func NewSampler(interval time.Duration) *Sampler {
	return &Sampler{interval: interval}
}

func (s *Sampler) Allow() (bool, int64) {
	now := time.Now().UnixNano()
	nextAllowed := s.nextAllowedUnixNano.Load()
	if now >= nextAllowed && s.nextAllowedUnixNano.CompareAndSwap(nextAllowed, now+int64(s.interval)) {
		return true, s.suppressed.Swap(0)
	}
	s.suppressed.Add(1)
	return false, 0
}
