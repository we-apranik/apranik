package logging_test

import (
	"sync"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/logging"
)

func TestSamplerAllowsOnePerIntervalAndCountsTheRest(t *testing.T) {
	sampler := logging.NewSampler(time.Hour)

	if allowed, suppressed := sampler.Allow(); !allowed || suppressed != 0 {
		t.Fatalf("first event: allowed=%v suppressed=%d, want true 0", allowed, suppressed)
	}
	for range 5 {
		if allowed, _ := sampler.Allow(); allowed {
			t.Fatal("an event inside the interval was allowed")
		}
	}
}

func TestSamplerReportsWhatItHeldBack(t *testing.T) {
	sampler := logging.NewSampler(20 * time.Millisecond)
	sampler.Allow()
	for range 3 {
		sampler.Allow()
	}

	time.Sleep(30 * time.Millisecond)
	if allowed, suppressed := sampler.Allow(); !allowed || suppressed != 3 {
		t.Errorf("after the interval: allowed=%v suppressed=%d, want true 3", allowed, suppressed)
	}
}

// Many goroutines hitting it at once still get exactly one event through.
func TestSamplerAllowsExactlyOneUnderContention(t *testing.T) {
	sampler := logging.NewSampler(time.Hour)
	var (
		wait    sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)
	for range 64 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if ok, _ := sampler.Allow(); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wait.Wait()
	if allowed != 1 {
		t.Errorf("%d events allowed, want 1", allowed)
	}
}
