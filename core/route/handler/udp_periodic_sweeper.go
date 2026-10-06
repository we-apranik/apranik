package handler

import (
	"sync"
	"time"
)

// What the sweeper calls on every tick: drop whatever has been idle too long,
// or add what a session has counted to the shared counters.
type periodicSweepable interface {
	sweep()
}

// One goroutine for every UDP session of a handler, instead of timers per
// session. It runs only while a session is registered, so a handler that
// serves no UDP holds no goroutine.
type periodicSweeper struct {
	interval time.Duration

	mu      sync.Mutex
	members map[periodicSweepable]struct{}
	stop    chan struct{} // non-nil while the goroutine runs
}

func newPeriodicSweeper(interval time.Duration) *periodicSweeper {
	return &periodicSweeper{interval: interval, members: make(map[periodicSweepable]struct{})}
}

func (s *periodicSweeper) add(member periodicSweepable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members[member] = struct{}{}
	if s.stop == nil {
		s.stop = make(chan struct{})
		go s.sweepUntilStopped(s.stop)
	}
}

func (s *periodicSweeper) remove(member periodicSweepable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.members, member)
	if len(s.members) == 0 && s.stop != nil {
		close(s.stop)
		s.stop = nil
	}
}

func (s *periodicSweeper) sweepUntilStopped(stop <-chan struct{}) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			members := make([]periodicSweepable, 0, len(s.members))
			for member := range s.members {
				members = append(members, member)
			}
			s.mu.Unlock()
			for _, member := range members {
				member.sweep()
			}
		case <-stop:
			return
		}
	}
}

// A quarter of the idle time, so something is forgotten at most a quarter
// after it became idle, and nothing outlives its setting by more than that.
func sweepIntervalFor(idleTimeout time.Duration) time.Duration {
	return max(idleTimeout/4, time.Millisecond)
}

// How often every UDP session adds what it counted privately to the shared
// counters, so a reader's numbers lag by about this much. PROVISIONAL: 1 second.
const defaultCountFlushInterval = time.Second

// A UDP session's private counts, flushed on every tick.
type sessionCountFlusher struct{ session interface{ FlushCounts() } }

func (f sessionCountFlusher) sweep() { f.session.FlushCounts() }
