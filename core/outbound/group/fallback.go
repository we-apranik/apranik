package group

import (
	"time"

	"context"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

type FallbackConfig struct {
	Tag string

	// Built and owned by the caller: closing the group never closes them.
	Members []component.Outbound

	Probe ProbeConfig

	Logger *logging.Logger
}

type fallbackGroup struct {
	memberSet
	prober *prober
}

func NewFallback(config FallbackConfig) (component.ProbingOutbound, error) {
	members, err := newMemberSet(config.Tag, config.Members, config.Logger)
	if err != nil {
		return nil, err
	}
	prober, err := newProber(config.Tag, config.Members, config.Probe, firstThatPassed{}, members.logger)
	if err != nil {
		return nil, err
	}
	return &fallbackGroup{memberSet: members, prober: prober}, nil
}

func (g *fallbackGroup) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	return g.dialStreamInOrder(ctx, metadata, g.prober.order())
}

func (g *fallbackGroup) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	return g.createDatagramInOrder(ctx, metadata, g.prober.order())
}

func (g *fallbackGroup) StartProbing(ctx context.Context) { g.prober.StartProbing(ctx) }

func (g *fallbackGroup) Close() error { return g.prober.Close() }

func (g *fallbackGroup) CurrentMemberTag() string {
	return g.members[g.prober.order()[0]].Tag()
}

type firstThatPassed struct{}

func (firstThatPassed) next(_ *memberHealth, passed []bool, latency []time.Duration) *memberHealth {
	return &memberHealth{
		passed:  passed,
		latency: latency,
		order:   orderPassedFirst(passed, func(int, int) int { return 0 }),
	}
}
