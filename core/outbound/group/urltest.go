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

	Tolerance time.Duration

	Logger *logging.Logger
}

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

func (g *urlTestGroup) CurrentMemberTag() string {
	return g.members[g.prober.order()[0]].Tag()
}

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
