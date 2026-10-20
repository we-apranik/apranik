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

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

type StreamConfig struct {
	Tag string

	ServerAddr spec.Destination

	Dialer      spec.StreamDialer
	ProxyClient spec.StreamProxyClient

	DatagramOverStreamClient spec.DatagramOverStreamProxyClient

	XUDPGlobalID *XUDPGlobalIDDeriver

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
		globalID:       config.XUDPGlobalID,
		logger:         logger.With("outbound", config.Tag),
	}
}

type streamOutbound struct {
	tag            string
	serverAddr     spec.Destination
	dialer         spec.StreamDialer
	proxyClient    spec.StreamProxyClient
	datagramClient spec.DatagramOverStreamProxyClient
	globalID       *XUDPGlobalIDDeriver
	logger         *logging.Logger
}

func (o *streamOutbound) Tag() string { return o.tag }

func (o *streamOutbound) Networks() []spec.Network {
	if o.datagramClient != nil {
		return []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
	}
	return []spec.Network{spec.NetworkTCP}
}

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

func (o *streamOutbound) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	if o.datagramClient == nil {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, spec.ErrUnsupportedNetwork)
	}

	switch placement := o.datagramClient.DatagramDestinationIsWritten(); placement {
	case spec.DatagramDestinationInEveryDatagram:
		if o.globalID != nil {
			metadata.XUDPGlobalID = o.globalID.For(metadata.Transport.SourceEndpoint)
		}
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
