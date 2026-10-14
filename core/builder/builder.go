// Package builder turns a blueprint into running inbounds and outbounds. It
// composes each transport stack as a fold, base first and each overlay
// wrapping what is below, so it never needs to know what any layer is. Stream
// overlays wrap one stream at a time: outbound, a dialer runs them after the
// base dials; inbound, core/inbound runs them per accepted connection.
package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/core/inbound"
	"github.com/we-apranik/apranik/core/inbound/mixed"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/outbound"
	"github.com/we-apranik/apranik/core/outbound/group"
	"github.com/we-apranik/apranik/core/registry"
	"github.com/we-apranik/apranik/core/route/dataset"
	"github.com/we-apranik/apranik/core/route/router"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/module/transport/base/udp"
	"github.com/we-apranik/apranik/spec"
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
	logging *logging.LoggerManager
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

	// One secret key per built core, so a UDP session's Global ID is the same for
	// every outbound and different from the ID of any other core.
	xudpGlobalIDDeriver *outbound.XUDPGlobalIDDeriver
}

// New builds the logging system first, so every component it goes on to
// build is handed a logger.
func New(log blueprint.Log) (*Builder, error) {
	system, err := logging.New(log)
	if err != nil {
		return nil, fmt.Errorf("logging: %w", err)
	}
	deriver, err := outbound.NewXUDPGlobalIDDeriver()
	if err != nil {
		system.Close()
		return nil, err
	}
	return &Builder{logging: system, logger: system.Logger(), resolver: dns.NewSystemResolver(system.Logger()), xudpGlobalIDDeriver: deriver}, nil
}

func (b *Builder) Logger() *logging.Logger { return b.logger }

// SubscribeToLog starts a live view of the log; see logging.LoggerManager.SubscribeToLog.
func (b *Builder) SubscribeToLog(minimumLevel logging.Level, bufferedRecords int) (*logging.Subscription, error) {
	return b.logging.SubscribeToLog(minimumLevel, bufferedRecords)
}

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
		statsConfig.TimeDials = section.DialHealth
	}
	statsConfig.TrackLiveConversations = coreBlueprint.IsLiveTrackingEnabled()
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
		MaxTCPConnections:        types.ValueOrDefault(inboundBlueprint.MaxTCPConnections, 0),
		MaxUDPSessions:           types.ValueOrDefault(inboundBlueprint.MaxUDPSessions, 0),
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
			IdleTimeout: types.ValueOrDefault(inboundBlueprint.TCPIdleTimeout, 0),
		}
	}

	b.logger.With("module", InboundProtocolName(inboundBlueprint)).
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
		// Before the server is built, so it is built knowing what UDP it takes. Check
		// has refused a muxcool key on a protocol that cannot carry it; with no key,
		// a protocol that can is told the defaults.
		if configurable, ok := inboundBlueprint.StreamProxy.(spec.MuxCoolConfigurable); ok {
			if err := configurable.ConfigureMuxCool(inboundBlueprint.MuxCool.Options()); err != nil {
				return nil, fmt.Errorf("proxy %q: %w", inboundBlueprint.StreamProxy.Info().Name, err)
			}
		}
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
	if err := checkMixedHalvesHaveNoUsers(socks5Server, httpServer); err != nil {
		return nil, err
	}
	server := mixed.NewStreamServer(socks5Server, httpServer, types.ValueOrDefault(inboundBlueprint.Mixed.FirstByteTimeout, 0))
	if err := addMixedUsers(server, inboundBlueprint.Mixed.Users); err != nil {
		return nil, err
	}
	return server, nil
}

// mixed is one inbound with one list of users, which lives at its own level; a
// user written under socks5 or http would be known to one protocol only.
func checkMixedHalvesHaveNoUsers(socks5Server, httpServer spec.StreamProxyServer) error {
	halves := []struct {
		name   string
		server spec.StreamProxyServer
	}{{mixedSOCKS5Name, socks5Server}, {mixedHTTPName, httpServer}}
	for _, half := range halves {
		if manager, manages := half.server.(spec.UserManager); manages && len(manager.ListUsers()) > 0 {
			return fmt.Errorf("mixed: users belong to mixed, not under %s: write them once, in mixed's own users", half.name)
		}
	}
	return nil
}

// Before the listener exists, so no client is ever accepted without them. Each
// user goes to both protocols, which apply their own rules; the entry and the
// protocol that refused it are named.
func addMixedUsers(server spec.StreamProxyServer, users []json.RawMessage) error {
	if len(users) == 0 {
		return nil
	}
	manager, manages := server.(spec.UserManager)
	if !manages {
		return errors.New("mixed: users need socks5 and http modules that can change their users")
	}
	for index, user := range users {
		if _, err := manager.AddUser(user); err != nil {
			return fmt.Errorf("mixed: users[%d]: %w", index, err)
		}
	}
	return nil
}

const (
	mixedSOCKS5Name = "socks5"
	mixedHTTPName   = "http"
)

func InboundProtocolName(inboundBlueprint blueprint.Inbound) string {
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
	groupBlueprint := outboundBlueprint.Group
	probe := group.ProbeConfig{
		URL:              groupBlueprint.Probe.URL,
		Interval:         types.ValueOrDefault(groupBlueprint.Probe.Interval, 0),
		Timeout:          types.ValueOrDefault(groupBlueprint.Probe.Timeout, 0),
		ExpectedStatuses: groupBlueprint.Probe.ExpectedStatuses,
	}

	var built component.Outbound
	var err error
	switch groupBlueprint.Type {
	case blueprint.GroupTypeURLTest:
		built, err = group.NewURLTest(group.URLTestConfig{
			Tag: outboundBlueprint.Tag, Members: members, Probe: probe, Tolerance: types.ValueOrDefault(groupBlueprint.Tolerance, 0), Logger: b.logger,
		})
	case blueprint.GroupTypeFallback:
		built, err = group.NewFallback(group.FallbackConfig{
			Tag: outboundBlueprint.Tag, Members: members, Probe: probe, Logger: b.logger,
		})
	case blueprint.GroupTypeSelector:
		built, err = group.NewSelector(group.SelectorConfig{Tag: outboundBlueprint.Tag, Members: members, Logger: b.logger})
	default:
		err = fmt.Errorf("outbound %q: unknown group type %q", outboundBlueprint.Tag, groupBlueprint.Type)
	}
	if err != nil {
		return nil, err
	}
	b.logger.Debug("Outbound group built", "outbound", outboundBlueprint.Tag, "type", groupBlueprint.Type, "members", len(members))
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
		MaxDatagramSize: types.ValueOrDefault(freedom.NATMaxDatagramSize, 0),
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

	// Before any client is built, so each one is built knowing how UDP goes out.
	// Check has refused a muxcool key on a protocol that cannot carry it; with no
	// key, a protocol that can is told the defaults.
	if configurable, ok := outboundBlueprint.Proxy.(spec.MuxCoolConfigurable); ok {
		if err := configurable.ConfigureMuxCool(outboundBlueprint.MuxCool.Options()); err != nil {
			dialer.Close()
			return nil, fmt.Errorf("outbound %q: proxy %q: %w", outboundBlueprint.Tag, outboundBlueprint.Proxy.Info().Name, err)
		}
	}

	// Likewise: Check has refused a proxy_request_timing key on a protocol that
	// cannot carry it, and one without a key is told the defaults.
	if configurable, ok := outboundBlueprint.Proxy.(spec.ProxyRequestTimingConfigurable); ok {
		if err := configurable.ConfigureProxyRequestTiming(outboundBlueprint.ProxyRequestTiming.Options()); err != nil {
			dialer.Close()
			return nil, fmt.Errorf("outbound %q: proxy %q: %w", outboundBlueprint.Tag, outboundBlueprint.Proxy.Info().Name, err)
		}
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

	var globalIDDeriver *outbound.XUDPGlobalIDDeriver
	if _, carriesMuxCool := outboundBlueprint.Proxy.(spec.MuxCoolConfigurable); carriesMuxCool && outboundBlueprint.MuxCool.XUDPGlobalIDEnabled() {
		globalIDDeriver = b.xudpGlobalIDDeriver
	}

	return outbound.NewStream(outbound.StreamConfig{
		Tag:                      outboundBlueprint.Tag,
		ServerAddr:               outboundBlueprint.Server,
		Dialer:                   dialer,
		ProxyClient:              proxyClient,
		DatagramOverStreamClient: datagramClient,
		XUDPGlobalID:             globalIDDeriver,
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
