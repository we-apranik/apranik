package group

import (
	"context"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

type URLTestConfig struct {
	Tag string

	// Built and owned by the caller: closing the group never closes them.
	Members []component.Outbound

	Probe ProbeConfig

	// How much faster than the current member another must be before the group
	// switches to it. Zero means the group's default.
	Tolerance time.Duration

	// Nil is allowed: a silent logger is substituted.
	Logger *logging.Logger
}

// urlTestGroup is an outbound that connects nowhere itself. For each
// connection it tries its members, the one whose probe answered fastest first,
// and returns the first that connects, so a member that fails between two
// probes costs one connection a retry, not an error.
type urlTestGroup struct {
	memberSet
	prober *prober
}

func NewURLTest(config URLTestConfig) (component.ProbingOutbound, error) {
	members, err := newMemberSet(config.Tag, config.Members, config.Logger)
	if err != nil {
		return nil, err
	}
	tolerance := config.Tolerance
	if tolerance == 0 {
		tolerance = defaultURLTestTolerance
	}
	prober, err := newProber(config.Tag, config.Members, config.Probe, fastestWithinTolerance{tolerance: tolerance}, members.logger)
	if err != nil {
		return nil, err
	}
	return &urlTestGroup{memberSet: members, prober: prober}, nil
}

func (g *urlTestGroup) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	return g.dialStreamInOrder(ctx, metadata, g.prober.order())
}

func (g *urlTestGroup) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	return g.createDatagramInOrder(ctx, metadata, g.prober.order())
}

func (g *urlTestGroup) StartProbing(ctx context.Context) { g.prober.StartProbing(ctx) }

func (g *urlTestGroup) Close() error { return g.prober.Close() }

// CurrentMemberTag is the member the probes favour now.
func (g *urlTestGroup) CurrentMemberTag() string {
	return g.members[g.prober.order()[0]].Tag()
}

// fastestWithinTolerance keeps the current member unless it failed or another
// is faster by more than the tolerance; when no probe passed the current one
// stays, which is the last good member.
type fastestWithinTolerance struct{ tolerance time.Duration }

func (s fastestWithinTolerance) next(previous *memberHealth, passed []bool, latency []time.Duration) *memberHealth {
	current := 0
	if previous != nil {
		current = previous.preferred
		fastest := -1
		for index := range passed {
			if passed[index] && (fastest < 0 || latency[index] < latency[fastest]) {
				fastest = index
			}
		}
		switch {
		case fastest < 0:
		case !passed[current], latency[current]-latency[fastest] > s.tolerance:
			current = fastest
		}
	}

	next := &memberHealth{passed: passed, latency: latency, preferred: current}
	next.order = orderPassedFirst(passed, func(a, b int) int {
		switch {
		case a == current && b != current && passed[a]:
			return -1
		case b == current && a != current && passed[b]:
			return 1
		default:
			return int(latency[a] - latency[b])
		}
	})
	return next
}
