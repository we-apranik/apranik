package group

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/outbound"
	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

type ProbeConfig struct {
	URL              string
	Interval         time.Duration
	Timeout          time.Duration
	ExpectedStatuses []httpstatusranges.Range
}

// memberHealth is the result of one probe round, replaced whole so a
// connection reads a consistent view without a lock. Both slices run parallel
// to the group's members. Before the first round every member counts as
// passed.
type memberHealth struct {
	passed  []bool
	latency []time.Duration

	preferred int

	order []int
}

type probeStrategy interface {
	next(previous *memberHealth, passed []bool, latency []time.Duration) *memberHealth
}

type prober struct {
	tag      string
	members  []component.Outbound
	tester   outbound.DelayTester
	interval time.Duration
	timeout  time.Duration
	strategy probeStrategy
	logger   *logging.Logger

	health atomic.Pointer[memberHealth]

	lifecycleMutex sync.Mutex
	probingStarted bool
	closed         bool
	stopProbing    context.CancelFunc
	probingEnded   chan struct{}
}

func newProber(tag string, members []component.Outbound, probe ProbeConfig, strategy probeStrategy, logger *logging.Logger) (*prober, error) {
	if probe.Interval < 0 || probe.Timeout < 0 {
		return nil, fmt.Errorf("outbound %q: a probe's interval and timeout must not be negative", tag)
	}
	url, interval, timeout, expected := probe.URL, probe.Interval, probe.Timeout, probe.ExpectedStatuses
	if url == "" {
		url = defaultGroupProbeURL
	}
	if interval == 0 {
		interval = defaultGroupProbeInterval
	}
	if timeout == 0 {
		timeout = defaultGroupProbeTimeout
	}
	if len(expected) == 0 {
		expected = defaultGroupProbeExpectedStatuses()
	}
	tester, err := outbound.NewDelayTester(url, expected)
	if err != nil {
		return nil, fmt.Errorf("outbound %q: %w", tag, err)
	}

	p := &prober{tag: tag, members: members, tester: tester, interval: interval, timeout: timeout, strategy: strategy, logger: logger}
	allPassed := make([]bool, len(members))
	for index := range allPassed {
		allPassed[index] = true
	}
	p.health.Store(strategy.next(nil, allPassed, make([]time.Duration, len(members))))
	return p, nil
}

func (p *prober) order() []int { return p.health.Load().order }

func (p *prober) StartProbing(ctx context.Context) {
	p.lifecycleMutex.Lock()
	defer p.lifecycleMutex.Unlock()
	if p.probingStarted || p.closed {
		return
	}
	p.probingStarted = true

	ctx, p.stopProbing = context.WithCancel(ctx)
	p.probingEnded = make(chan struct{})
	go p.probeRepeatedly(ctx)
}

func (p *prober) probeRepeatedly(ctx context.Context) {
	defer close(p.probingEnded)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		p.probeEveryMemberOnce(ctx)
		timer.Reset(p.interval)
	}
}

func (p *prober) probeEveryMemberOnce(ctx context.Context) {
	passed := make([]bool, len(p.members))
	latency := make([]time.Duration, len(p.members))
	failures := make([]error, len(p.members))

	var probes sync.WaitGroup
	for index, member := range p.members {
		probes.Add(1)
		go func() {
			defer probes.Done()
			latency[index], failures[index] = p.tester.MeasureDelay(ctx, member, p.timeout)
			passed[index] = failures[index] == nil
		}()
	}
	probes.Wait()
	if ctx.Err() != nil {
		return
	}

	for index, member := range p.members {
		if passed[index] {
			p.logger.Debug("Probe passed", "member", member.Tag(), "latency", latency[index])
		} else {
			p.logger.Debug("Probe failed", "member", member.Tag(), "error", failures[index])
		}
	}

	previous := p.health.Load()
	next := p.strategy.next(previous, passed, latency)
	p.health.Store(next)

	if before, after := previous.order[0], next.order[0]; before != after {
		p.logger.Info("Group switched member", "from", p.members[before].Tag(), "to", p.members[after].Tag())
	}
}

func (p *prober) Close() error {
	p.lifecycleMutex.Lock()
	p.closed = true
	stopProbing, probingEnded := p.stopProbing, p.probingEnded
	p.lifecycleMutex.Unlock()

	if stopProbing != nil {
		stopProbing()
		<-probingEnded
	}
	return nil
}

func orderPassedFirst(passed []bool, less func(a, b int) int) []int {
	order := make([]int, len(passed))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if passed[a] != passed[b] {
			if passed[a] {
				return -1
			}
			return 1
		}
		return less(a, b)
	})
	return order
}
