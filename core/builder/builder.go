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
	"github.com/The-Apranik/Apranik/core/dns"
	"github.com/The-Apranik/Apranik/core/inbound"
	"github.com/The-Apranik/Apranik/core/inbound/mixed"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/core/stats"
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

	// What every base transport resolves through. The system resolver until
	// UseDNS replaces it.
	resolver *dns.Resolver

	// UseDNS must come before any outbound, which takes the resolver.
	outboundsBuilt bool

	// Nil until BuildStats, and nil when the blueprint switches counting off:
	// nothing is counted then.
	stats *stats.Registry

	// Built here, not from the blueprint, so closed here: the internal
	// freedom DNS servers without an outbound go through.
	internalOutbounds []component.Outbound
}

// New builds the logging system first, so every component it goes on to
// build is handed a logger.
func New(log blueprint.Log) (*Builder, error) {
	system, err := logging.New(log)
	if err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}
	return &Builder{logging: system, logger: system.Logger(), resolver: dns.NewSystemResolver(system.Logger())}, nil
}

func (b *Builder) Logger() *logging.Logger { return b.logger }

// BuildStats makes the registry every inbound and handler counts into. Counting
// is on unless the blueprint's stats section disables it, so a blueprint
// written in Go with no section counts everything. Call it before BuildInbound
// and hand Stats to each handler; BuildRouting calls it for run and Verify.
func (b *Builder) BuildStats(coreBlueprint *blueprint.Blueprint) *stats.Registry {
	section := coreBlueprint.Stats
	if section != nil && section.Disabled {
		b.stats = nil
		b.resolver.SetStats(nil)
		return nil
	}

	statsConfig := stats.Config{DNSServerTags: []string{dns.SystemServerTag}}
	if coreBlueprint.DNS != nil {
		for _, server := range coreBlueprint.DNS.Servers {
			statsConfig.DNSServerTags = append(statsConfig.DNSServerTags, server.Tag)
		}
	}
	for _, inbound := range coreBlueprint.Inbounds {
		statsConfig.InboundTags = append(statsConfig.InboundTags, inbound.Tag)
	}
	for _, outbound := range coreBlueprint.Outbounds {
		statsConfig.Outbounds = append(statsConfig.Outbounds, stats.OutboundDescription{Tag: outbound.Tag, IsGroup: outbound.Group != nil})
	}
	if coreBlueprint.Routing != nil {
		for index := range coreBlueprint.Routing.Rules {
			statsConfig.RuleLocations = append(statsConfig.RuleLocations, router.RuleLocation(index))
		}
	}
	statsConfig.RuleLocations = append(statsConfig.RuleLocations, router.DefaultRuleLocation)
	if section != nil {
		statsConfig.TrackedInboundTags = section.Inbounds
		statsConfig.TrackedOutboundTags = section.Outbounds
		statsConfig.TrackedUsers = section.Users
		statsConfig.TrackLiveConnections = section.LiveConnections != nil && *section.LiveConnections
		statsConfig.TimeDials = section.DialHealth
	}
	b.stats = stats.NewRegistry(statsConfig)
	b.resolver.SetStats(b.stats.DNSCounters())
	return b.stats
}

// Stats is the registry BuildStats made: nil before it, and when counting is
// off. Its reads are how an embedder gets the numbers.
func (b *Builder) Stats() *stats.Registry { return b.stats }

// Close closes the resolver and the DNS servers it holds, the outbounds this
// builder made for them, then the log.
func (b *Builder) Close() error {
	errs := []error{b.resolver.Close()}
	for _, internal := range b.internalOutbounds {
		errs = append(errs, internal.Close())
	}
	errs = append(errs, b.logging.Close())
	return errors.Join(errs...)
}

// BuildInbound builds everything an inbound needs and binds its listener last,
// so a refusal only a constructor can make (a certificate that fails to load)
// leaves no port open.
func (b *Builder) BuildInbound(inboundBlueprint blueprint.Inbound) (component.Inbound, error) {
	prepared, err := b.prepareInbound(inboundBlueprint)
	if err != nil {
		return nil, err
	}

	listener, err := buildStreamListener(inboundBlueprint.StreamTransport, inboundBlueprint.Bind)
	if err != nil {
		prepared.discard()
		return nil, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, err)
	}

	return inbound.NewStream(inbound.StreamConfig{
		Tag:                      inboundBlueprint.Tag,
		Listener:                 listener,
		Overlays:                 prepared.overlays,
		ProxyServer:              prepared.proxyServer,
		DatagramOverStreamServer: prepared.datagramServer,
		Fallback:                 prepared.fallback,
		AssociateSocket:          b.buildUDPAssociateSocket(inboundBlueprint.Bind),
		MaxTCPConnections:        inboundBlueprint.MaxTCPConnections,
		MaxUDPSessions:           inboundBlueprint.MaxUDPSessions,
		Counters:                 b.stats.Inbound(inboundBlueprint.Tag),
		Logger:                   b.logger,
	}), nil
}

// CheckInbound makes every refusal BuildInbound makes except the bind, which
// says something about the machine and not about the config.
func (b *Builder) CheckInbound(inboundBlueprint blueprint.Inbound) error {
	prepared, err := b.prepareInbound(inboundBlueprint)
	if err != nil {
		return err
	}
	prepared.discard()
	return nil
}

// preparedInbound is an inbound with everything built except its socket.
type preparedInbound struct {
	overlays       []inbound.StreamOverlay
	proxyServer    spec.StreamProxyServer
	datagramServer spec.DatagramOverStreamProxyServer
	fallback       *inbound.Fallback
}

func (p preparedInbound) discard() {
	if p.fallback != nil {
		p.fallback.Dialer.Close()
	}
}

func (b *Builder) prepareInbound(inboundBlueprint blueprint.Inbound) (preparedInbound, error) {
	if inboundBlueprint.Network == spec.NetworkUDP {
		return preparedInbound{}, fmt.Errorf("inbound %q: a datagram inbound needs a datagram proxy, and this build registers none: %w",
			inboundBlueprint.Tag, spec.ErrUnsupportedNetwork)
	}
	if inboundBlueprint.StreamTransport.Base == nil {
		return preparedInbound{}, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, errNoBaseTransport)
	}

	var prepared preparedInbound
	var err error
	if prepared.overlays, err = buildStreamOverlayServers(inboundBlueprint.StreamTransport); err != nil {
		return preparedInbound{}, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, err)
	}

	if prepared.proxyServer, err = buildInboundProxyServer(inboundBlueprint); err != nil {
		return preparedInbound{}, fmt.Errorf("inbound %q: %w", inboundBlueprint.Tag, err)
	}

	if inboundBlueprint.DatagramOverStreamProxy != nil {
		if prepared.datagramServer, err = inboundBlueprint.DatagramOverStreamProxy.NewDatagramOverStreamServer(); err != nil {
			return preparedInbound{}, fmt.Errorf("inbound %q: proxy %q: %w", inboundBlueprint.Tag, inboundBlueprint.DatagramOverStreamProxy.Info().Name, err)
		}
	}

	// Always plain TCP, straight to the fallback: never the inbound's
	// own stack, and no lookup by name.
	if inboundBlueprint.Fallback.IsValid() {
		dialer, err := buildStreamDialer(blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()}, b.resolver)
		if err != nil {
			return preparedInbound{}, fmt.Errorf("inbound %q: fallback: %w", inboundBlueprint.Tag, err)
		}
		prepared.fallback = &inbound.Fallback{
			Dialer:      dialer,
			Destination: inboundBlueprint.Fallback,
			IdleTimeout: inboundBlueprint.TCPIdleTimeout,
		}
	}

	b.logger.With("module", inboundProtocolName(inboundBlueprint)).
		Debug("Stream proxy server built", "inbound", inboundBlueprint.Tag, "udp", prepared.datagramServer != nil, "fallback", prepared.fallback != nil)
	return prepared, nil
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
	return mixed.NewStreamServer(socks5Server, httpServer, inboundBlueprint.Mixed.FirstByteTimeout), nil
}

func inboundProtocolName(inboundBlueprint blueprint.Inbound) string {
	if inboundBlueprint.Mixed != nil {
		return registry.MixedProtocolName
	}
	return inboundBlueprint.StreamProxy.Info().Name
}

func (b *Builder) BuildOutbound(outboundBlueprint blueprint.Outbound) (component.Outbound, error) {
	b.outboundsBuilt = true
	switch {
	case outboundBlueprint.Freedom != nil:
		return b.buildFreedomOutbound(outboundBlueprint)
	case outboundBlueprint.Proxy != nil:
		return b.buildProxyOutbound(outboundBlueprint)
	case outboundBlueprint.Group != nil:
		return nil, fmt.Errorf("outbound %q is a group, which needs its members built first: use BuildOutbounds", outboundBlueprint.Tag)
	default:
		return nil, fmt.Errorf("outbound %q has no protocol", outboundBlueprint.Tag)
	}
}

// BuildOutbounds builds every outbound, in the order given: the first is the
// default when there is no routing section. Groups are built after the
// outbounds they pick among, whatever order the config file lists them in.
func (b *Builder) BuildOutbounds(outboundBlueprints []blueprint.Outbound) ([]component.Outbound, error) {
	built := make([]component.Outbound, len(outboundBlueprints))
	builtByTag := make(map[string]component.Outbound, len(outboundBlueprints))
	closeBuilt := func() {
		for _, outbound := range built {
			if outbound != nil {
				outbound.Close()
			}
		}
	}

	pendingGroups := make([]int, 0)
	for index, outboundBlueprint := range outboundBlueprints {
		if outboundBlueprint.Group != nil {
			pendingGroups = append(pendingGroups, index)
			continue
		}
		outbound, err := b.BuildOutbound(outboundBlueprint)
		if err != nil {
			closeBuilt()
			return nil, err
		}
		built[index], builtByTag[outboundBlueprint.Tag] = outbound, outbound
	}

	for len(pendingGroups) > 0 {
		stillPending := pendingGroups[:0:0]
		for _, index := range pendingGroups {
			outboundBlueprint := outboundBlueprints[index]
			members, ready := membersBuiltSoFar(outboundBlueprint.Group.Members, builtByTag)
			if !ready {
				stillPending = append(stillPending, index)
				continue
			}
			outbound, err := b.buildGroupOutbound(outboundBlueprint, members)
			if err != nil {
				closeBuilt()
				return nil, err
			}
			built[index], builtByTag[outboundBlueprint.Tag] = outbound, outbound
		}
		if len(stillPending) == len(pendingGroups) {
			closeBuilt()
			return nil, fmt.Errorf("outbound %q: members that are missing or wait on each other", outboundBlueprints[stillPending[0]].Tag)
		}
		pendingGroups = stillPending
	}
	return built, nil
}

func membersBuiltSoFar(tags []string, builtByTag map[string]component.Outbound) ([]component.Outbound, bool) {
	members := make([]component.Outbound, 0, len(tags))
	for _, tag := range tags {
		member, found := builtByTag[tag]
		if !found {
			return nil, false
		}
		members = append(members, member)
	}
	return members, true
}

func (b *Builder) buildGroupOutbound(outboundBlueprint blueprint.Outbound, members []component.Outbound) (component.Outbound, error) {
	group := outboundBlueprint.Group
	built, err := outbound.NewGroup(outbound.GroupConfig{
		Tag:          outboundBlueprint.Tag,
		Members:      members,
		PicksFastest: group.Type == blueprint.GroupTypeURLTest,
		Probe: outbound.ProbeConfig{
			UseTLS:   group.Probe.Type == blueprint.ProbeTypeHTTPS,
			URL:      group.Probe.URL,
			Interval: group.Probe.Interval,
			Timeout:  group.Probe.Timeout,
		},
		Tolerance: group.Tolerance,
		Logger:    b.logger,
	})
	if err != nil {
		return nil, err
	}
	b.logger.Debug("Outbound group built", "outbound", outboundBlueprint.Tag, "type", group.Type, "members", len(members))
	return built, nil
}

func (b *Builder) buildFreedomOutbound(outboundBlueprint blueprint.Outbound) (component.Outbound, error) {
	freedom := outboundBlueprint.Freedom

	var (
		dialer         spec.StreamDialer
		datagramDialer spec.DatagramDialer
		err            error
	)
	if !freedom.StreamTransport.IsZero() {
		if dialer, err = buildStreamDialer(freedom.StreamTransport, b.resolver); err != nil {
			return nil, fmt.Errorf("outbound %q: %w", outboundBlueprint.Tag, err)
		}
	}
	if !freedom.DatagramTransport.IsZero() {
		if datagramDialer, err = buildDatagramDialer(freedom.DatagramTransport, b.resolver); err != nil {
			if dialer != nil {
				dialer.Close()
			}
			return nil, fmt.Errorf("outbound %q: %w", outboundBlueprint.Tag, err)
		}
	}

	return outbound.NewFreedom(outbound.FreedomConfig{
		Tag:             outboundBlueprint.Tag,
		Dialer:          dialer,
		DatagramDialer:  datagramDialer,
		DatagramPolicy:  freedom.NAT,
		MaxDatagramSize: freedom.NATMaxDatagramSize,
		Counters:        b.stats.Outbound(outboundBlueprint.Tag),
		Logger:          b.logger,
	}), nil
}

func (b *Builder) buildProxyOutbound(outboundBlueprint blueprint.Outbound) (component.Outbound, error) {
	// The proxy's own name never goes through the rules, which may send it
	// to a DNS server that goes through this very proxy.
	var serverResolver spec.Resolver = b.resolver
	if outboundBlueprint.DNSServer != "" {
		serverResolver = b.resolver.PinnedTo(outboundBlueprint.DNSServer)
	}
	dialer, err := buildStreamDialer(outboundBlueprint.StreamTransport, serverResolver)
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

		listener, err := buildDatagramListener(stack, bind, b.resolver)
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

func buildStreamDialer(stack blueprint.StreamTransportStack, resolver spec.Resolver) (spec.StreamDialer, error) {
	if stack.Base == nil {
		return nil, errNoBaseTransport
	}
	dialer, err := stack.Base.NewStreamDialer(resolver)
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

func buildDatagramDialer(stack blueprint.DatagramTransportStack, resolver spec.Resolver) (spec.DatagramDialer, error) {
	if stack.Base == nil {
		return nil, errNoBaseTransport
	}
	dialer, err := stack.Base.NewDatagramDialer(resolver)
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

func buildDatagramListener(stack blueprint.DatagramTransportStack, bind spec.Endpoint, resolver spec.Resolver) (spec.DatagramListener, error) {
	if stack.Base == nil {
		return nil, errNoBaseTransport
	}
	listener, err := stack.Base.NewDatagramListener(bind, resolver)
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

// LoadDatasets reads every dataset the blueprint declares, once, for the
// router and the DNS rules alike.
func (b *Builder) LoadDatasets(declarations []blueprint.Dataset) (map[string]*dataset.Dataset, error) {
	datasets, errs := dataset.LoadAll(declarations)
	if err := dataset.Joined(declarations, errs); err != nil {
		return nil, err
	}
	for _, declaration := range declarations {
		b.logger.Debug("Dataset loaded", "tag", declaration.Tag, "entries", datasets[declaration.Tag].Len())
	}
	return datasets, nil
}

// BuildRouter compiles the routing section's rules against the outbounds
// already built and the datasets loaded. With no routing section, every
// request takes the first outbound.
func (b *Builder) BuildRouter(routing *blueprint.Routing, outbounds []component.Outbound, datasets map[string]*dataset.Dataset) (component.Router, error) {
	if len(outbounds) == 0 {
		return nil, errors.New("router: no outbounds to route to")
	}
	if routing == nil {
		return router.NewWithDefaultOutbound(outbounds[0]), nil
	}

	outboundsByTag := make(map[string]component.Outbound, len(outbounds))
	for _, outbound := range outbounds {
		outboundsByTag[outbound.Tag()] = outbound
	}
	return router.New(router.Config{Routing: *routing, Outbounds: outboundsByTag, Datasets: datasets})
}
