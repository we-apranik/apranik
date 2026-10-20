package outbound

import (
	"context"
	"fmt"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/udp"
	"github.com/we-apranik/apranik/core/net/udp/nat"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

type FreedomConfig struct {
	Tag            string
	Dialer         spec.StreamDialer
	DatagramDialer spec.DatagramDialer

	DatagramPolicy *nat.Policy

	MaxDatagramSize int

	Counters *stats.OutboundCounters

	Logger *logging.Logger
}

func NewFreedom(config FreedomConfig) component.Outbound {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	return &freedomOutbound{
		tag:            config.Tag,
		streamDialer:   config.Dialer,
		datagramDialer: config.DatagramDialer,
		datagramPolicy: config.DatagramPolicy,
		counters:       config.Counters,
		replyBuffers:   udp.NewBufferPool(config.MaxDatagramSize),
		logger:         logger.With("outbound", config.Tag),
	}
}

type freedomOutbound struct {
	tag            string
	streamDialer   spec.StreamDialer
	datagramDialer spec.DatagramDialer
	datagramPolicy *nat.Policy
	counters       *stats.OutboundCounters

	replyBuffers *udp.BufferPool
	logger       *logging.Logger
}

func (o *freedomOutbound) Tag() string { return o.tag }

func (o *freedomOutbound) Networks() []spec.Network {
	networks := make([]spec.Network, 0, 2)
	if o.streamDialer != nil {
		networks = append(networks, spec.NetworkTCP)
	}
	if o.datagramDialer != nil {
		networks = append(networks, spec.NetworkUDP)
	}
	return networks
}

func (o *freedomOutbound) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	if o.streamDialer == nil {
		return nil, fmt.Errorf("outbound %q has no stream transport: %w", o.tag, spec.ErrUnsupportedNetwork)
	}
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, spec.ErrUnsupportedNetwork)
	}

	destinationStream, err := o.streamDialer.DialStream(ctx, metadata.Destination)
	if err != nil {
		o.logger.Debug("Dial failed", "error", err)
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return destinationStream, nil
}

func (o *freedomOutbound) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	if o.datagramDialer == nil {
		return nil, fmt.Errorf("outbound %q has no datagram transport: %w", o.tag, spec.ErrUnsupportedNetwork)
	}

	policy := nat.NewDefaultPolicy()
	if o.datagramPolicy != nil {
		policy = *o.datagramPolicy
	}

	o.logger.Debug("NAT datagram created",
		"mapping", policy.Mapping(), "filtering", policy.Filtering())

	dial := func(ctx context.Context) (spec.Datagram, error) {
		return o.datagramDialer.CreateDatagram(ctx, spec.Endpoint{})
	}

	options := []nat.Option{nat.WithBuffers(o.replyBuffers)}
	if o.counters != nil {
		options = append(options,
			nat.WithReplyRejected(func() { o.counters.DatagramDroppedFor(stats.OutboundDropRejectedByNAT) }),
			nat.WithReplyTooLarge(func() { o.counters.DatagramDroppedFor(stats.OutboundDropTooLargeFromOutbound) }))
	}
	natDatagram, err := nat.New(ctx, dial, policy, o.logger, options...)
	if err != nil {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return natDatagram, nil
}

func (o *freedomOutbound) Close() error { return nil }
