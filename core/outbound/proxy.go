package outbound

import (
	"context"
	"fmt"
	"time"

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

	// Zero means the Default constants above; negative means no bound.
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration

	// Nil is allowed: NewStream substitutes a silent logger.
	Logger *logging.Logger
}

func NewStream(config StreamConfig) component.Outbound {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	return &streamOutbound{
		tag:              config.Tag,
		serverAddr:       config.ServerAddr,
		dialer:           config.Dialer,
		proxyClient:      config.ProxyClient,
		datagramClient:   config.DatagramOverStreamClient,
		dialTimeout:      config.DialTimeout,
		handshakeTimeout: config.HandshakeTimeout,
		logger:           logger.With("outbound", config.Tag),
	}
}

// dialer is a field, not an embedded interface: component.Outbound.DialStream and
// spec.StreamDialer.DialStream share a name at different signatures, so
// embedding would not compile.
type streamOutbound struct {
	tag              string
	serverAddr       spec.Destination
	dialer           spec.StreamDialer
	proxyClient      spec.StreamProxyClient
	datagramClient   spec.DatagramOverStreamProxyClient
	dialTimeout      time.Duration
	handshakeTimeout time.Duration
	logger           *logging.Logger
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

	carrier, err := o.dialServer(ctx)
	if err != nil {
		return nil, err
	}

	handshakeCtx, endHandshake := boundedContext(ctx, o.handshakeTimeout, DefaultHandshakeTimeout)
	payload, err := o.proxyClient.RequestStream(handshakeCtx, carrier, metadata)
	endHandshake()
	if err != nil {
		carrier.Close()
		o.logger.Debug("Proxy handshake failed", "error", err)
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return payload, nil
}

// One stream to the server per call: the UDP session calls this once per
// client session, and every destination's datagrams share it.
func (o *streamOutbound) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	if o.datagramClient == nil {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, spec.ErrUnsupportedNetwork)
	}

	carrier, err := o.dialServer(ctx)
	if err != nil {
		return nil, err
	}

	handshakeCtx, endHandshake := boundedContext(ctx, o.handshakeTimeout, DefaultHandshakeTimeout)
	datagram, err := o.datagramClient.RequestDatagramOverStream(handshakeCtx, carrier, metadata)
	endHandshake()
	if err != nil {
		carrier.Close()
		o.logger.Debug("Proxy handshake failed", "error", err)
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return datagram, nil
}

func (o *streamOutbound) dialServer(ctx context.Context) (spec.Stream, error) {
	dialCtx, endDial := boundedContext(ctx, o.dialTimeout, DefaultDialTimeout)
	carrier, err := o.dialer.DialStream(dialCtx, o.serverAddr)
	endDial()
	if err != nil {
		o.logger.Debug("Dial to proxy server failed", "error", err)
		return nil, fmt.Errorf("outbound %q: reaching %s: %w", o.tag, o.serverAddr, err)
	}
	return carrier, nil
}

func (o *streamOutbound) Close() error { return nil }
