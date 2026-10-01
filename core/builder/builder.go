// Package builder turns a blueprint into running inbounds and outbounds. It
// composes each transport stack as a fold, base first and each overlay
// wrapping what is below, so it never needs to know what any layer is. Stream
// overlays wrap one stream at a time: outbound, a dialer runs them after the
// base dials; inbound, core/inbound runs them per accepted connection.
package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/inbound"
	"github.com/The-Apranik/Apranik/core/inbound/mixed"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/module/transport/base/tcp"
	"github.com/The-Apranik/Apranik/module/transport/base/udp"
	"github.com/The-Apranik/Apranik/spec"
)

// A blueprint written in Go need not have passed config.Check, so a missing
// piece is an error here rather than a nil dereference.
var (
	errNoInboundProtocol = errors.New("no proxy protocol")
	errNoBaseTransport   = errors.New("the transport stack has no base transport")
)

// Builder holds no registry: every module in a Config is already constructed
// and validated, so there is nothing left to look up or dispatch on.
type Builder struct {
	logging *logging.System
	logger  *logging.Logger
}

// New builds the logging system first, so every component it goes on to
// build is handed a logger.
func New(log blueprint.Log) (*Builder, error) {
	system, err := logging.New(log)
	if err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}
	return &Builder{logging: system, logger: system.Logger()}, nil
}

func (b *Builder) Logger() *logging.Logger { return b.logger }

func (b *Builder) Close() error { return b.logging.Close() }

func (b *Builder) BuildInbound(inboundBlueprint blueprint.Inbound) (component.Inbound, error) {
	if inboundBlueprint.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("inbound %q: a datagram inbound needs a datagram proxy, and this build registers none: %w",
			inboundBlueprint.Tag, spec.ErrUnsupportedNetwork)
	}

	// Before the listener: a certificate that fails to load leaves no port
	// open.
	overlays, err := buildStreamOverlayServers(inboundBlueprint.StreamTransport)
	if err != nil {
		return nil, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, err)
	}

	listener, err := buildStreamListener(inboundBlueprint.StreamTransport, inboundBlueprint.Bind)
	if err != nil {
		return nil, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, err)
	}

	proxyServer, err := buildInboundProxyServer(inboundBlueprint)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, err)
	}

	var datagramServer spec.DatagramOverStreamProxyServer
	if inboundBlueprint.DatagramOverStreamProxy != nil {
		if datagramServer, err = inboundBlueprint.DatagramOverStreamProxy.NewDatagramOverStreamServer(); err != nil {
			listener.Close()
			return nil, fmt.Errorf("inbound %q: proxy %q: %w", inboundBlueprint.Tag, inboundBlueprint.DatagramOverStreamProxy.Info().Name, err)
		}
	}

	// Always plain TCP, straight to the fallback: never the inbound's
	// own stack, and no lookup by name.
	var fallback *inbound.Fallback
	if inboundBlueprint.Fallback.IsValid() {
		dialer, err := buildStreamDialer(blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()})
		if err != nil {
			listener.Close()
			return nil, fmt.Errorf("inbound %q: fallback: %w", inboundBlueprint.Tag, err)
		}
		fallback = &inbound.Fallback{
			Dialer:      dialer,
			Destination: inboundBlueprint.Fallback,
			IdleTimeout: inboundBlueprint.TCPIdleTimeout,
		}
	}

	b.logger.With("module", inboundProtocolName(inboundBlueprint)).
		Debug("Stream proxy server built", "inbound", inboundBlueprint.Tag, "udp", datagramServer != nil, "fallback", fallback != nil)

	return inbound.NewStream(inbound.StreamConfig{
		Tag:                      inboundBlueprint.Tag,
		Listener:                 listener,
		Overlays:                 overlays,
		ProxyServer:              proxyServer,
		DatagramOverStreamServer: datagramServer,
		Fallback:                 fallback,
		AssociateSocket:          b.buildUDPAssociateSocket(inboundBlueprint.Bind),
		HandshakeTimeout:         inboundBlueprint.HandshakeTimeout,
		MaxConnections:           inboundBlueprint.MaxConnections,
		Logger:                   b.logger,
	}), nil
}

func buildInboundProxyServer(inboundBlueprint blueprint.Inbound) (spec.StreamProxyServer, error) {
	if inboundBlueprint.Mixed == nil && inboundBlueprint.StreamProxy == nil {
		return nil, errNoInboundProtocol
	}
	if inboundBlueprint.Mixed != nil && (inboundBlueprint.Mixed.SOCKS5 == nil || inboundBlueprint.Mixed.HTTP == nil) {
		return nil, fmt.Errorf("mixed: %w: it needs both socks5 and http", errNoInboundProtocol)
	}
	if inboundBlueprint.Mixed == nil {
		server, err := inboundBlueprint.StreamProxy.NewStreamServer()
		if err != nil {
			return nil, fmt.Errorf("proxy %q: %w", inboundBlueprint.StreamProxy.Info().Name, err)
		}
		return server, nil
	}

	socks5Server, err := inboundBlueprint.Mixed.SOCKS5.NewStreamServer()
	if err != nil {
		return nil, fmt.Errorf("mixed: proxy %q: %w", inboundBlueprint.Mixed.SOCKS5.Info().Name, err)
	}
	httpServer, err := inboundBlueprint.Mixed.HTTP.NewStreamServer()
	if err != nil {
		return nil, fmt.Errorf("mixed: proxy %q: %w", inboundBlueprint.Mixed.HTTP.Info().Name, err)
	}
	return mixed.NewStreamServer(socks5Server, httpServer), nil
}

func inboundProtocolName(inboundBlueprint blueprint.Inbound) string {
	if inboundBlueprint.Mixed != nil {
		return registry.MixedProtocolName
	}
	return inboundBlueprint.StreamProxy.Info().Name
}

func (b *Builder) BuildOutbound(outboundBlueprint blueprint.Outbound) (component.Outbound, error) {
	switch {
	case outboundBlueprint.Freedom != nil:
		return b.buildFreedomOutbound(outboundBlueprint)
	case outboundBlueprint.Proxy != nil:
		return b.buildProxyOutbound(outboundBlueprint)
	default:
		return nil, fmt.Errorf("outbound %q has no protocol", outboundBlueprint.Tag)
	}
}

func (b *Builder) buildFreedomOutbound(outboundBlueprint blueprint.Outbound) (component.Outbound, error) {
	freedom := outboundBlueprint.Freedom

	var (
		dialer         spec.StreamDialer
		datagramDialer spec.DatagramDialer
		err            error
	)
	if !freedom.StreamTransport.IsZero() {
		if dialer, err = buildStreamDialer(freedom.StreamTransport); err != nil {
			return nil, fmt.Errorf("outbound %q: %w", outboundBlueprint.Tag, err)
		}
	}
	if !freedom.DatagramTransport.IsZero() {
		if datagramDialer, err = buildDatagramDialer(freedom.DatagramTransport); err != nil {
			if dialer != nil {
				dialer.Close()
			}
			return nil, fmt.Errorf("outbound %q: %w", outboundBlueprint.Tag, err)
		}
	}

	return outbound.NewFreedom(outbound.FreedomConfig{
		Tag:            outboundBlueprint.Tag,
		DialTimeout:    outboundBlueprint.DialTimeout,
		Dialer:         dialer,
		DatagramDialer: datagramDialer,
		DatagramPolicy: freedom.NAT,
		Logger:         b.logger,
	}), nil
}

func (b *Builder) buildProxyOutbound(outboundBlueprint blueprint.Outbound) (component.Outbound, error) {
	dialer, err := buildStreamDialer(outboundBlueprint.StreamTransport)
	if err != nil {
		return nil, fmt.Errorf("outbound %q: %w", outboundBlueprint.Tag, err)
	}

	proxyClient, err := outboundBlueprint.Proxy.NewStreamClient()
	if err != nil {
		dialer.Close()
		return nil, fmt.Errorf("outbound %q: proxy %q: %w", outboundBlueprint.Tag, outboundBlueprint.Proxy.Info().Name, err)
	}

	var datagramClient spec.DatagramOverStreamProxyClient
	if outboundBlueprint.DatagramOverStreamProxy != nil {
		if datagramClient, err = outboundBlueprint.DatagramOverStreamProxy.NewDatagramOverStreamClient(); err != nil {
			dialer.Close()
			return nil, fmt.Errorf("outbound %q: proxy %q: %w", outboundBlueprint.Tag, outboundBlueprint.Proxy.Info().Name, err)
		}
	}

	b.logger.With("module", outboundBlueprint.Proxy.Info().Name).
		Debug("Stream proxy client built", "outbound", outboundBlueprint.Tag, "udp", datagramClient != nil)

	return outbound.NewStream(outbound.StreamConfig{
		Tag:                      outboundBlueprint.Tag,
		ServerAddr:               outboundBlueprint.Server,
		Dialer:                   dialer,
		ProxyClient:              proxyClient,
		DatagramOverStreamClient: datagramClient,
		DialTimeout:              outboundBlueprint.DialTimeout,
		HandshakeTimeout:         outboundBlueprint.HandshakeTimeout,
		Logger:                   b.logger,
	}), nil
}

// Always plain UDP: no configurability, no lookup by name. A fresh socket per
// association, each on its own ephemeral port, on the same address the inbound
// listens on. Lazy: a proxy that never does ASSOCIATE never opens one.
func (b *Builder) buildUDPAssociateSocket(bindAddr spec.Endpoint) spec.UDPAssociateSocketFunc {
	// The inbound's address, but its own port: zero means ephemeral.
	bind, bindErr := spec.NewEndpoint(bindAddr.Addr(), 0)
	stack := blueprint.DatagramTransportStack{Base: udp.NewBaseDatagramTransportModule()}

	return func(ctx context.Context) (spec.Datagram, error) {
		if bindErr != nil {
			return nil, fmt.Errorf("opening a UDP associate socket: %w", bindErr)
		}

		listener, err := buildDatagramListener(stack, bind)
		if err != nil {
			return nil, fmt.Errorf("opening a UDP associate socket on %s: %w", bind, err)
		}
		socket, err := listener.ListenDatagram(ctx)
		if err != nil {
			listener.Close()
			return nil, err
		}
		return socket, nil
	}
}

func buildStreamDialer(stack blueprint.StreamTransportStack) (spec.StreamDialer, error) {
	if stack.Base == nil {
		return nil, errNoBaseTransport
	}
	dialer, err := stack.Base.NewStreamDialer()
	if err != nil {
		return nil, fmt.Errorf("transport %q: %w", stack.Base.Info().Name, err)
	}
	if len(stack.Overlays) == 0 {
		return dialer, nil
	}

	clients := make([]namedStreamOverlayClient, 0, len(stack.Overlays))
	for _, overlay := range stack.Overlays {
		client, err := overlay.NewStreamOverlayClient()
		if err != nil {
			dialer.Close()
			return nil, fmt.Errorf("transport %q: %w", overlay.Info().Name, err)
		}
		clients = append(clients, namedStreamOverlayClient{name: overlay.Info().Name, client: client})
	}
	return &overlayStreamDialer{underlyingDialer: dialer, clients: clients}, nil
}

type namedStreamOverlayClient struct {
	name   string
	client spec.StreamOverlayClient
}

// overlayStreamDialer dials through the base, then runs each overlay's client
// side in stack order over the stream, all within the caller's context.
type overlayStreamDialer struct {
	underlyingDialer spec.StreamDialer
	clients          []namedStreamOverlayClient
}

func (d *overlayStreamDialer) DialStream(ctx context.Context, server spec.Destination) (spec.Stream, error) {
	stream, err := d.underlyingDialer.DialStream(ctx, server)
	if err != nil {
		return nil, err
	}
	for _, overlay := range d.clients {
		wrapped, err := overlay.client.WrapClientStream(ctx, stream, server)
		if err != nil {
			stream.Close()
			return nil, fmt.Errorf("transport %q: %w", overlay.name, err)
		}
		stream = wrapped
	}
	return stream, nil
}

func (d *overlayStreamDialer) Close() error { return d.underlyingDialer.Close() }

// Binds here, not at activation: LocalAddr must return a real address, and a
// port conflict should be a startup error a user can read. Only the base:
// overlays run per connection in core/inbound.
func buildStreamListener(stack blueprint.StreamTransportStack, bind spec.Endpoint) (spec.StreamListener, error) {
	if stack.Base == nil {
		return nil, errNoBaseTransport
	}
	listener, err := stack.Base.NewStreamListener(bind)
	if err != nil {
		return nil, fmt.Errorf("transport %q: %w", stack.Base.Info().Name, err)
	}
	return listener, nil
}

func buildStreamOverlayServers(stack blueprint.StreamTransportStack) ([]inbound.StreamOverlay, error) {
	overlays := make([]inbound.StreamOverlay, 0, len(stack.Overlays))
	for _, overlay := range stack.Overlays {
		server, err := overlay.NewStreamOverlayServer()
		if err != nil {
			return nil, fmt.Errorf("transport %q: %w", overlay.Info().Name, err)
		}
		overlays = append(overlays, inbound.StreamOverlay{Name: overlay.Info().Name, Server: server})
	}
	return overlays, nil
}

func buildDatagramDialer(stack blueprint.DatagramTransportStack) (spec.DatagramDialer, error) {
	if stack.Base == nil {
		return nil, errNoBaseTransport
	}
	dialer, err := stack.Base.NewDatagramDialer()
	if err != nil {
		return nil, fmt.Errorf("transport %q: %w", stack.Base.Info().Name, err)
	}

	for _, overlay := range stack.Overlays {
		wrapped, err := overlay.WrapDatagramDialer(dialer)
		if err != nil {
			dialer.Close()
			return nil, fmt.Errorf("transport %q: %w", overlay.Info().Name, err)
		}
		dialer = wrapped
	}
	return dialer, nil
}

func buildDatagramListener(stack blueprint.DatagramTransportStack, bind spec.Endpoint) (spec.DatagramListener, error) {
	if stack.Base == nil {
		return nil, errNoBaseTransport
	}
	listener, err := stack.Base.NewDatagramListener(bind)
	if err != nil {
		return nil, fmt.Errorf("transport %q: %w", stack.Base.Info().Name, err)
	}

	for _, overlay := range stack.Overlays {
		wrapped, err := overlay.WrapDatagramListener(listener)
		if err != nil {
			listener.Close()
			return nil, fmt.Errorf("transport %q: %w", overlay.Info().Name, err)
		}
		listener = wrapped
	}
	return listener, nil
}

// BuildRouter loads the routing section's datasets and compiles its rules
// against the outbounds already built. With no routing section, every
// request takes the first outbound.
func (b *Builder) BuildRouter(routing *blueprint.Routing, outbounds []component.Outbound) (component.Router, error) {
	if len(outbounds) == 0 {
		return nil, errors.New("router: no outbounds to route to")
	}
	if routing == nil {
		return router.NewWithDefaultOutbound(outbounds[0]), nil
	}

	datasets, errs := dataset.LoadAll(routing.Datasets)
	if err := dataset.Joined(routing.Datasets, errs); err != nil {
		return nil, fmt.Errorf("router: %w", err)
	}
	for _, declaration := range routing.Datasets {
		b.logger.Debug("Dataset loaded", "tag", declaration.Tag, "entries", datasets[declaration.Tag].Len())
	}

	outboundsByTag := make(map[string]component.Outbound, len(outbounds))
	for _, outbound := range outbounds {
		outboundsByTag[outbound.Tag()] = outbound
	}
	return router.New(router.Config{Routing: *routing, Outbounds: outboundsByTag, Datasets: datasets})
}
