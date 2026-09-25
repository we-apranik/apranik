package outbound

import (
	"context"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/spec"
)

type StreamConfig struct {
	Tag string

	// The proxy server's own address, not the user's destination.
	ServerAddr spec.Destination

	Dialer      spec.StreamDialer
	ProxyClient spec.StreamProxyClient

	// Zero means the Default constants above; negative means no bound.
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration

	// Nil is allowed: NewStream substitutes a silent logger.
	Logger *logging.Logger
}

func NewStream(config StreamConfig) spec.Outbound {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	return &streamOutbound{
		tag:              config.Tag,
		serverAddr:       config.ServerAddr,
		dialer:           config.Dialer,
		proxyClient:      config.ProxyClient,
		dialTimeout:      config.DialTimeout,
		handshakeTimeout: config.HandshakeTimeout,
		logger:           logger.With("outbound", config.Tag),
	}
}

// dialer is a field, not an embedded interface: spec.Outbound.DialStream and
// spec.StreamDialer.DialStream share a name at different signatures, so
// embedding would not compile.
type streamOutbound struct {
	tag              string
	serverAddr       spec.Destination
	dialer           spec.StreamDialer
	proxyClient      spec.StreamProxyClient
	dialTimeout      time.Duration
	handshakeTimeout time.Duration
	logger           *logging.Logger
}

func (o *streamOutbound) Tag() string { return o.tag }

// TCP only: no proxy client shape in spec can carry UDP without also dialing.
func (o *streamOutbound) Networks() []spec.Network {
	return []spec.Network{spec.NetworkTCP}
}

// This is where the meaning of an address flips: the transport stack gets the
// proxy server's address, and the user's destination is seen only by the
// proxy client, which encodes it into the preamble.
func (o *streamOutbound) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("outbound %q: %w", o.tag, spec.ErrUnsupportedNetwork)
	}

	dialCtx, endDial := boundedContext(ctx, o.dialTimeout, DefaultDialTimeout)
	carrier, err := o.dialer.DialStream(dialCtx, o.serverAddr)
	endDial()
	if err != nil {
		o.logger.Debug("dial to proxy server failed", "error", err)
		return nil, fmt.Errorf("outbound %q: reaching %s: %w", o.tag, o.serverAddr, err)
	}

	handshakeCtx, endHandshake := boundedContext(ctx, o.handshakeTimeout, DefaultHandshakeTimeout)
	payload, err := o.proxyClient.ConnectStream(handshakeCtx, carrier, metadata)
	endHandshake()
	if err != nil {
		carrier.Close()
		o.logger.Debug("proxy handshake failed", "error", err)
		return nil, fmt.Errorf("outbound %q: %w", o.tag, err)
	}
	return payload, nil
}

func (o *streamOutbound) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	return nil, fmt.Errorf("outbound %q: %w", o.tag, spec.ErrUnsupportedNetwork)
}

func (o *streamOutbound) Close() error { return nil }
