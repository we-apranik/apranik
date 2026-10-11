// Package outbound is where traffic leaves Apranik: through a proxy server, or
// straight to the destination with freedom. Blocking is a routing action, not
// an outbound. Each outbound states in advance
// which networks it can carry, so nothing is routed to it that it cannot serve.
//
// Nothing here bounds a dial or a handshake: every transport and protocol
// bounds its own, with the timeouts in its own settings.
package outbound

import (
	"context"
	"fmt"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/spec"
)

type StreamConfig struct {
	Tag string

	// The proxy server's own address, not the user's destination.
	ServerAddr spec.Destination

	Dialer      spec.StreamDialer
	ProxyClient spec.StreamProxyClient

	// Nil means this outbound carries TCP only.
	DatagramOverStreamClient spec.DatagramOverStreamProxyClient

	// Nil is allowed: NewStream substitutes a silent logger.
	Logger *logging.Logger
}

func NewStream(config StreamConfig) component.Outbound {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	return &streamOutbound{
		tag:            config.Tag,
		serverAddr:     config.ServerAddr,
		dialer:         config.Dialer,
		proxyClient:    config.ProxyClient,
		datagramClient: config.DatagramOverStreamClient,
		logger:         logger.With("outbound", config.Tag),
	}
}

// dialer is a field, not an embedded interface: component.Outbound.DialStream and
// spec.StreamDialer.DialStream share a name at different signatures, so
// embedding would not compile.
type streamOutbound struct {
	tag            string
	serverAddr     spec.Destination
	dialer         spec.StreamDialer
	proxyClient    spec.StreamProxyClient
	datagramClient spec.DatagramOverStreamProxyClient
	logger         *logging.Logger
}

func (o *streamOutbound) Tag() string { return o.tag }

// UDP only when the protocol frames datagrams inside its streams, so routing
// rules this outbound out before a packet arrives rather than on a failed dial.
func (o *streamOutbound) Networks() []spec.Network {
	if o.datagramClient != nil {
		return []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
	}
	return []spec.Network{spec.NetworkTCP}
}

// This is where the meaning of an address flips: the transport stack gets the
// proxy server's address, and the user's destination is seen only by the
// proxy client, which encodes it into the preamble.
func (o *streamOutbound) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, spec.ErrUnsupportedNetwork)
	}

	serverStream, err := o.dialServer(ctx)
	if err != nil {
		return nil, err
	}

	payload, err := o.proxyClient.RequestStream(ctx, serverStream, metadata)
	if err != nil {
		serverStream.Close()
		o.logger.Debug("Proxy handshake failed", "error", err)
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return payload, nil
}

// The UDP session calls this once per client session. Where the protocol
// writes the destination of a datagram decides what it returns: one stream to
// the server that every destination's datagrams share, or, when the protocol
// names the destination once in the stream request, a datagram that opens a
// stream per destination.
func (o *streamOutbound) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	if o.datagramClient == nil {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, spec.ErrUnsupportedNetwork)
	}

	switch placement := o.datagramClient.DatagramDestinationIsWritten(); placement {
	case spec.DatagramDestinationInEveryDatagram:
		return o.createDatagramOnOneStream(ctx, metadata)
	case spec.DatagramDestinationInStreamRequest:
		return newDatagramOverStreamPerDestination(ctx, o, metadata), nil
	default:
		return nil, fmt.Errorf("outbound %q: the proxy client says %d for where it writes the datagram destination, which is neither value of spec.DatagramDestinationPlacement", o.tag, placement)
	}
}

func (o *streamOutbound) createDatagramOnOneStream(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	serverStream, err := o.dialServer(ctx)
	if err != nil {
		return nil, err
	}

	datagram, err := o.datagramClient.RequestDatagramOverStream(ctx, serverStream, metadata)
	if err != nil {
		serverStream.Close()
		o.logger.Debug("Proxy handshake failed", "error", err)
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return datagram, nil
}

func (o *streamOutbound) dialServer(ctx context.Context) (spec.Stream, error) {
	serverStream, err := o.dialer.DialStream(ctx, o.serverAddr)
	if err != nil {
		o.logger.Debug("Dial to proxy server failed", "error", err)
		return nil, fmt.Errorf("outbound %q: reaching %s: %w", o.tag, o.serverAddr, err)
	}
	return serverStream, nil
}

func (o *streamOutbound) Close() error { return nil }
