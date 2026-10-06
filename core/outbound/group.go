package outbound

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/spec"
)

// PROVISIONAL.
const DefaultGroupTolerance = 50 * time.Millisecond

type GroupConfig struct {
	Tag string

	// Built and owned by the caller: closing the group never closes them.
	Members []component.Outbound

	// True picks the member that answered its probe fastest (urltest); false
	// picks the first member in list order that passed it (fallback).
	PicksFastest bool

	Probe ProbeConfig

	// Only with PicksFastest. Zero means DefaultGroupTolerance.
	Tolerance time.Duration

	// Nil is allowed: NewGroup substitutes a silent logger.
	Logger *logging.Logger
}

// NewGroup is an outbound that connects nowhere itself. For each connection it
// tries its members in the order its probes favour and returns the first that
// connects, so a member that fails between two probes costs one connection a
// retry, not an error. It probes only after StartProbing: until the first round
// ends it uses list order.
func NewGroup(config GroupConfig) (component.ProbingOutbound, error) {
	if len(config.Members) == 0 {
		return nil, fmt.Errorf("outbound %q: a group needs at least one member", config.Tag)
	}
	target, err := newProbeTarget(config.Probe)
	if err != nil {
		return nil, fmt.Errorf("outbound %q: %w", config.Tag, err)
	}

	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	interval, timeout, tolerance := config.Probe.Interval, config.Probe.Timeout, config.Tolerance
	if interval == 0 {
		interval = DefaultProbeInterval
	}
	if timeout == 0 {
		timeout = DefaultProbeTimeout
	}
	if tolerance == 0 {
		tolerance = DefaultGroupTolerance
	}

	g := &group{
		tag:          config.Tag,
		members:      config.Members,
		picksFastest: config.PicksFastest,
		target:       target,
		interval:     interval,
		timeout:      timeout,
		tolerance:    tolerance,
		logger:       logger.With("outbound", config.Tag),
	}
	initial := &memberHealth{
		passed:  make([]bool, len(config.Members)),
		latency: make([]time.Duration, len(config.Members)),
	}
	for index := range initial.passed {
		initial.passed[index] = true
	}
	initial.order = g.preferredOrder(initial)
	g.health.Store(initial)
	return g, nil
}

// memberHealth is the result of one probe round, replaced whole so a
// connection reads a consistent view without a lock. Both slices run parallel
// to the group's members. Before the first round every member counts as
// passed.
type memberHealth struct {
	passed  []bool
	latency []time.Duration

	// The member urltest prefers: the last good one when no probe passes.
	// Unused by fallback.
	preferred int

	// Member indexes in the order a connection tries them, worked out once per
	// round so a connection allocates nothing to choose.
	order []int
}

type group struct {
	tag          string
	members      []component.Outbound
	picksFastest bool
	target       probeTarget
	interval     time.Duration
	timeout      time.Duration
	tolerance    time.Duration
	logger       *logging.Logger

	health atomic.Pointer[memberHealth]

	lifecycleMutex sync.Mutex
	probingStarted bool
	closed         bool
	stopProbing    context.CancelFunc
	probingEnded   chan struct{}
}

func (g *group) Tag() string { return g.tag }

// Only what every member carries, so a group never sends a connection through
// a member that treats it differently from the others: UDP through a group of
// a proxy and freedom would otherwise leave directly whenever the proxy was down.
func (g *group) Networks() []spec.Network {
	networks := slices.Clone(g.members[0].Networks())
	for _, member := range g.members[1:] {
		memberNetworks := member.Networks()
		networks = slices.DeleteFunc(networks, func(network spec.Network) bool {
			return !slices.Contains(memberNetworks, network)
		})
	}
	return networks
}

func (g *group) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	var failures []error
	for _, index := range g.health.Load().order {
		member := g.members[index]
		stream, err := member.DialStream(ctx, metadata)
		if err == nil {
			g.logger.Debug("Group member chosen", "member", member.Tag())
			return carriedStream{Stream: stream, carryingOutboundTags: component.TagsOfOutboundsThatCarried(member, stream)}, nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, g.everyMemberFailed(failures)
}

// A UDP session asks once and keeps the datagram, so the member is chosen once
// per session and a later probe never moves a session mid-flight.
func (g *group) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	var failures []error
	for _, index := range g.health.Load().order {
		member := g.members[index]
		datagram, err := member.CreateDatagram(ctx, metadata)
		if err == nil {
			g.logger.Debug("Group member chosen", "member", member.Tag())
			return carriedDatagram{Datagram: datagram, carryingOutboundTags: component.TagsOfOutboundsThatCarried(member, datagram)}, nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, g.everyMemberFailed(failures)
}

// The streams a group returns say which outbounds beneath it carried them
// (component.CarryingOutboundsReporter); every other call is the member's own.
type carriedStream struct {
	spec.Stream
	carryingOutboundTags []string
}

func (s carriedStream) CarryingOutboundTags() []string { return s.carryingOutboundTags }

// The datagrams a group returns say which outbounds beneath it carried them,
// as its streams do; every other call is the member's own.
type carriedDatagram struct {
	spec.Datagram
	carryingOutboundTags []string
}

func (d carriedDatagram) CarryingOutboundTags() []string { return d.carryingOutboundTags }

func (g *group) everyMemberFailed(failures []error) error {
	return fmt.Errorf("outbound %q: every member failed: %w", g.tag, errors.Join(failures...))
}

// Members that passed their probe come first and the rest follow, so a
// connection still tries a member whose probe failed rather than refusing when
// all have failed: a failed probe may be one lost packet.
func (g *group) preferredOrder(health *memberHealth) []int {
	order := make([]int, len(g.members))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case health.passed[a] != health.passed[b]:
			if health.passed[a] {
				return -1
			}
			return 1
		case g.picksFastest && a == health.preferred && b != health.preferred && health.passed[a]:
			return -1
		case g.picksFastest && b == health.preferred && a != health.preferred && health.passed[b]:
			return 1
		case g.picksFastest:
			return int(health.latency[a] - health.latency[b])
		default:
			return 0
		}
	})
	return order
}

func (g *group) StartProbing(ctx context.Context) {
	g.lifecycleMutex.Lock()
	defer g.lifecycleMutex.Unlock()
	if g.probingStarted || g.closed {
		return
	}
	g.probingStarted = true

	ctx, g.stopProbing = context.WithCancel(ctx)
	g.probingEnded = make(chan struct{})
	go g.probeRepeatedly(ctx)
}

func (g *group) probeRepeatedly(ctx context.Context) {
	defer close(g.probingEnded)
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		g.probeEveryMemberOnce(ctx)
		timer.Reset(g.interval)
	}
}

func (g *group) probeEveryMemberOnce(ctx context.Context) {
	passed := make([]bool, len(g.members))
	latency := make([]time.Duration, len(g.members))
	failures := make([]error, len(g.members))

	var probes sync.WaitGroup
	for index, member := range g.members {
		probes.Add(1)
		go func() {
			defer probes.Done()
			latency[index], failures[index] = g.target.measure(ctx, member, g.timeout)
			passed[index] = failures[index] == nil
		}()
	}
	probes.Wait()
	// A round cut short by Close says nothing about the members.
	if ctx.Err() != nil {
		return
	}

	for index, member := range g.members {
		if passed[index] {
			g.logger.Debug("Probe passed", "member", member.Tag(), "latency", latency[index])
		} else {
			g.logger.Debug("Probe failed", "member", member.Tag(), "error", failures[index])
		}
	}

	previous := g.health.Load()
	next := &memberHealth{passed: passed, latency: latency, preferred: previous.preferred}
	if g.picksFastest {
		next.preferred = g.fastestWorthSwitchingTo(previous.preferred, passed, latency)
	}
	next.order = g.preferredOrder(next)
	g.health.Store(next)

	if before, after := previous.order[0], next.order[0]; before != after {
		g.logger.Info("Group switched member", "from", g.members[before].Tag(), "to", g.members[after].Tag())
	}
}

// The member urltest prefers after a round: the current one, unless it failed
// or another is faster by more than the tolerance. When no probe passed the
// current one stays, which is the last good member.
func (g *group) fastestWorthSwitchingTo(current int, passed []bool, latency []time.Duration) int {
	fastest := -1
	for index := range g.members {
		if passed[index] && (fastest < 0 || latency[index] < latency[fastest]) {
			fastest = index
		}
	}
	switch {
	case fastest < 0:
		return current
	case !passed[current], latency[current]-latency[fastest] > g.tolerance:
		return fastest
	default:
		return current
	}
}

func (g *group) Close() error {
	g.lifecycleMutex.Lock()
	g.closed = true
	stopProbing, probingEnded := g.stopProbing, g.probingEnded
	g.lifecycleMutex.Unlock()

	if stopProbing != nil {
		stopProbing()
		<-probingEnded
	}
	return nil
}
