package outbound

import (
	"context"
	"fmt"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/spec"
)

type FreedomConfig struct {
	Tag            string
	Dialer         spec.StreamDialer
	DatagramDialer spec.DatagramDialer

	// Nil means nat.NewDefaultPolicy().
	DatagramPolicy *nat.Policy

	// Nil is allowed: NewFreedom substitutes a silent logger.
	Logger *logging.Logger
}

// Freedom is not a proxy protocol pretending to be one: it has no server and no
// preamble, and dials the address a proxy client would instead encode.
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
		logger:         logger.With("outbound", config.Tag),
	}
}

type freedomOutbound struct {
	tag            string
	streamDialer   spec.StreamDialer
	datagramDialer spec.DatagramDialer
	datagramPolicy *nat.Policy
	logger         *logging.Logger
}

func (o *freedomOutbound) Tag() string { return o.tag }

// Only what was configured, so routing can rule this outbound out before a
// packet arrives rather than on a failed dial.
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

// Dials nothing yet: the destination arrives per packet in WriteTo, not at
// dial time, so nat.Datagram dials lazily from there.
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

	// Zero local address: any address, any free port. A source address, if
	// this outbound ever takes one, is captured here.
	dial := func(ctx context.Context) (spec.Datagram, error) {
		return o.datagramDialer.CreateDatagram(ctx, spec.Endpoint{})
	}

	natDatagram, err := nat.New(ctx, dial, policy, o.logger)
	if err != nil {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return natDatagram, nil
}

func (o *freedomOutbound) Close() error { return nil }
