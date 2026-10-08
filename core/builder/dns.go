package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/dns"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/module/transport/base/tcp"
	"github.com/The-Apranik/Apranik/module/transport/base/udp"
	"github.com/The-Apranik/Apranik/spec"
)

var errDNSAfterOutbounds = errors.New("dns: UseDNS must come before the first outbound, which takes the resolver")

// Resolver is what every base transport resolves through, and what the
// routing resolve action asks.
func (b *Builder) Resolver() *dns.Resolver { return b.resolver }

// UseDNS is the first half of building the resolver: outbounds
// need it to be built, and its servers need the built outbounds, so it is
// created here, empty, and BuildDNS attaches the servers afterwards. A nil
// section keeps the system resolver.
func (b *Builder) UseDNS(section *blueprint.DNS) error {
	if section == nil {
		return nil
	}
	if b.outboundsBuilt {
		return errDNSAfterOutbounds
	}
	previous := b.resolver
	b.resolver = dns.NewResolver(dns.Config{IPVersions: section.IPVersions, Cache: section.Cache, Logger: b.logger})
	b.resolver.SetStats(b.stats.DNSCounters())
	return previous.Close()
}

// BuildDNS builds each DNS server over its outbound and attaches them to the
// resolver UseDNS created. Nothing to do for a nil section.
func (b *Builder) BuildDNS(section *blueprint.DNS, outbounds []component.Outbound, datasets map[string]*dataset.Dataset) error {
	if section == nil {
		return nil
	}
	outboundsByTag := make(map[string]component.Outbound, len(outbounds))
	for _, outbound := range outbounds {
		outboundsByTag[outbound.Tag()] = outbound
	}

	servers := make([]dns.Server, 0, len(section.Servers))
	closeBuilt := func() {
		for _, built := range servers {
			if built.Wire != nil {
				built.Wire.Close()
			}
		}
	}
	for _, serverBlueprint := range section.Servers {
		built, err := b.buildDNSServer(section, serverBlueprint, outboundsByTag)
		if err != nil {
			closeBuilt()
			return fmt.Errorf("dns server %q: %w", serverBlueprint.Tag, err)
		}
		servers = append(servers, built)
		b.logger.Debug("DNS server built", "tag", serverBlueprint.Tag, "outbound", serverBlueprint.Outbound)
	}
	if err := b.resolver.AttachServers(servers, section.Rules, section.DefaultServer, datasets); err != nil {
		closeBuilt()
		return err
	}
	return nil
}

func (b *Builder) buildDNSServer(section *blueprint.DNS, serverBlueprint blueprint.DNSServer, outboundsByTag map[string]component.Outbound) (dns.Server, error) {
	server := dns.Server{Tag: serverBlueprint.Tag, IPVersions: serverBlueprint.IPVersions}
	if serverBlueprint.System != nil {
		server.System = dns.NewSystemServer(types.ValueOrDefault(serverBlueprint.System.Timeout, 0))
		return server, nil
	}

	outbound, err := b.dnsServerOutbound(serverBlueprint.Outbound, outboundsByTag)
	if err != nil {
		return dns.Server{}, err
	}
	bootstrapServer := serverBlueprint.DNSServer
	if bootstrapServer == "" {
		bootstrapServer = section.DefaultServer
	}
	bootstrap := b.resolver.PinnedTo(bootstrapServer)

	switch {
	case serverBlueprint.Stream != nil:
		dialer := &dnsServerStreamDialer{outbound: outbound, bootstrap: bootstrap}
		if serverBlueprint.Stream.NeedsTLS() {
			if serverBlueprint.TLS == nil {
				return dns.Server{}, errors.New("needs TLS, and no tls module was given")
			}
			if dialer.tls, err = serverBlueprint.TLS.NewStreamOverlayClient(); err != nil {
				return dns.Server{}, fmt.Errorf("tls: %w", err)
			}
		}
		if server.Wire, err = serverBlueprint.Stream.NewStreamDNSServer(dialer); err != nil {
			return dns.Server{}, fmt.Errorf("%s: %w", serverBlueprint.Stream.Info().Name, err)
		}
	case serverBlueprint.Datagram != nil:
		dialer := &dnsServerDatagramDialer{outbound: outbound, bootstrap: bootstrap}
		if server.Wire, err = serverBlueprint.Datagram.NewDatagramDNSServer(dialer); err != nil {
			return dns.Server{}, fmt.Errorf("%s: %w", serverBlueprint.Datagram.Info().Name, err)
		}
	default:
		return dns.Server{}, errors.New("no type")
	}
	return server, nil
}

// dnsServerOutbound is the outbound a server named, or the internal freedom,
// built once and only when some server needs it.
func (b *Builder) dnsServerOutbound(tag string, outboundsByTag map[string]component.Outbound) (component.Outbound, error) {
	if tag == "" {
		tag = blueprint.DefaultDNSOutboundTag
	}
	if outbound, found := outboundsByTag[tag]; found {
		return outbound, nil
	}
	if tag != blueprint.DefaultDNSOutboundTag {
		return nil, fmt.Errorf("no outbound %q", tag)
	}
	internal, err := b.buildFreedomOutbound(blueprint.Outbound{Tag: tag, Freedom: &blueprint.Freedom{
		StreamTransport:   blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()},
		DatagramTransport: blueprint.DatagramTransportStack{Base: udp.NewBaseDatagramTransportModule()},
	}})
	if err != nil {
		return nil, err
	}
	b.internalOutbounds = append(b.internalOutbounds, internal)
	outboundsByTag[tag] = internal
	return internal, nil
}

// dnsServerStreamDialer is the dialer a stream DNS server module is handed:
// the server's outbound, with the server's own TLS on top. It holds the
// outbound as a field rather than being one: component.Outbound and
// spec.StreamDialer both have DialStream, with different signatures.
type dnsServerStreamDialer struct {
	outbound  component.Outbound
	bootstrap spec.Resolver
	tls       spec.StreamOverlayClient // nil without TLS
}

// DialStream resolves a hostname destination itself, with the server's
// dns_server, before dialing the outbound: core cannot see the address inside
// the module's settings, so this is where its bootstrap happens.
// TLS is still given the name, for its server name.
func (d *dnsServerStreamDialer) DialStream(ctx context.Context, destination spec.Destination) (spec.Stream, error) {
	ctx = dns.ContextWithOutboundInLookupPath(ctx, d.outbound.Tag())
	target, err := bootstrapDestination(ctx, d.bootstrap, destination)
	if err != nil {
		return nil, err
	}
	stream, err := d.outbound.DialStream(ctx, spec.Metadata{Network: spec.NetworkTCP, Destination: target})
	if err != nil {
		return nil, err
	}
	if d.tls == nil {
		return stream, nil
	}
	wrapped, err := d.tls.WrapClientStream(ctx, stream, destination)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("tls: %w", err)
	}
	return wrapped, nil
}

func (d *dnsServerStreamDialer) Close() error { return nil }

// dnsServerDatagramDialer is the dialer a datagram DNS server module is
// handed: the server's outbound's datagram path.
type dnsServerDatagramDialer struct {
	outbound  component.Outbound
	bootstrap spec.Resolver
}

func (d *dnsServerDatagramDialer) CreateDatagram(ctx context.Context, _ spec.Endpoint) (spec.Datagram, error) {
	ctx = dns.ContextWithOutboundInLookupPath(ctx, d.outbound.Tag())
	datagram, err := d.outbound.CreateDatagram(ctx, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		return nil, err
	}
	return &bootstrappingDatagram{Datagram: datagram, ctx: ctx, bootstrap: d.bootstrap}, nil
}

func (d *dnsServerDatagramDialer) Close() error { return nil }

// bootstrappingDatagram resolves a hostname it is asked to write to with the
// server's dns_server, as dnsServerStreamDialer does. Its context is the one
// query's: a DNS server's datagram lives for one query.
type bootstrappingDatagram struct {
	spec.Datagram
	ctx       context.Context
	bootstrap spec.Resolver
}

func (d *bootstrappingDatagram) WriteTo(payload []byte, to spec.Destination) (int, error) {
	target, err := bootstrapDestination(d.ctx, d.bootstrap, to)
	if err != nil {
		return 0, err
	}
	return d.Datagram.WriteTo(payload, target)
}

func bootstrapDestination(ctx context.Context, bootstrap spec.Resolver, destination spec.Destination) (spec.Destination, error) {
	domain, isDomain := destination.Domain()
	if !isDomain {
		return destination, nil
	}
	addresses, err := bootstrap.ResolveDomain(ctx, domain)
	if err != nil {
		return spec.Destination{}, fmt.Errorf("resolving the dns server %s: %w", domain, err)
	}
	if len(addresses) == 0 {
		return spec.Destination{}, fmt.Errorf("resolving the dns server %s: %w", domain, dns.ErrNoAddresses)
	}
	return spec.NewDestinationFromIP(addresses[0], destination.Port())
}

var (
	_ spec.StreamDialer   = (*dnsServerStreamDialer)(nil)
	_ spec.DatagramDialer = (*dnsServerDatagramDialer)(nil)
)
