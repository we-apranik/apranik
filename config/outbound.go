package config

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/core/registry"
)

const (
	freedomProtocol = registry.FreedomProtocolName
	blockProtocol   = registry.BlockProtocolName

	urlTestProtocol  = registry.URLTestProtocolName
	fallbackProtocol = registry.FallbackProtocolName
	selectorProtocol = registry.SelectorProtocolName

	// Named here as freedom's udp is: the base that reaches destinations.
	freedomBaseTransport = "tcp"
)

func (b *configBuilder) buildOutbound(raw rawOutbound, loc string) blueprint.Outbound {
	outbound := blueprint.Outbound{Tag: raw.Tag, DNSServer: raw.DNSServer}

	if raw.MovedDialTimeout != nil {
		b.diagnostics.errorf(fieldLocation(loc, "dial_timeout"),
			`dial_timeout moved to tcp's settings: "transport": [{"type": "tcp", "settings": {"dial_timeout": "10s"}}, ...]`)
	}
	if raw.MovedHandshakeTimeout != nil {
		b.diagnostics.errorf(fieldLocation(loc, "handshake_timeout"),
			`handshake_timeout moved into each layer's own settings: "protocol": {"type": "trojan", "settings": {"handshake_timeout": "10s"}}, and likewise for every transport`)
	}

	if raw.Server != "" {
		server, err := parseServerAddr(raw.Server, raw.ServerPort)
		if err != nil {
			b.diagnostics.report(fieldLocation(loc, "server"), err)
		} else {
			outbound.Server = server
		}
	}

	transportLoc := fieldLocation(loc, "transport")
	if len(raw.Transport) > 0 && raw.Protocol.Type != freedomProtocol {
		outbound.StreamTransport = b.buildStreamTransportStack(raw.Transport, transportLoc)
	}

	protocolLoc := fieldLocation(loc, "protocol")
	switch raw.Protocol.Type {
	case "":
		b.diagnostics.report(protocolLoc, errMissingField("type", "protocol"))
	case freedomProtocol:
		outbound.Freedom = b.buildFreedom(raw.Protocol.Settings, fieldLocation(protocolLoc, "settings"), raw.Transport, transportLoc)
	case urlTestProtocol, fallbackProtocol, selectorProtocol:
		outbound.Group = b.buildGroup(raw.Protocol, fieldLocation(protocolLoc, "settings"))
	case mixedProtocol:
		b.diagnostics.errorf(fieldLocation(protocolLoc, "type"), "mixed is an inbound protocol; an outbound needs a proxy protocol or freedom")
	case blockProtocol:
		b.diagnostics.errorf(fieldLocation(protocolLoc, "type"),
			`block is no longer an outbound; blocking is a routing action: {"match": {...}, "action": {"type": "block"}}`)
	default:
		outbound.Proxy = b.buildStreamProxy(raw.Protocol, protocolLoc)
		if outbound.Proxy != nil {
			outbound.DatagramOverStreamProxy = b.buildDatagramOverStreamProxy(raw.Protocol, protocolLoc)
		}
	}

	return outbound
}

// Whether a field makes sense is Check's; this reads what is written. Nil when
// the settings cannot be decoded.
func (b *configBuilder) buildGroup(protocol rawModule, loc string) *blueprint.Group {
	var settings rawGroupSettings
	if len(protocol.Settings) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(protocol.Settings))
		if b.strict {
			decoder.DisallowUnknownFields()
		}
		if err := decoder.Decode(&settings); err != nil {
			b.diagnostics.report(loc, err)
			return nil
		}
	}

	group := &blueprint.Group{
		Type:      blueprint.GroupType(protocol.Type),
		Members:   settings.Members,
		Tolerance: durationOrNil(settings.Tolerance),
	}
	if settings.Probe != nil && group.Type == blueprint.GroupTypeSelector {
		b.diagnostics.errorf(fieldLocation(loc, "probe"),
			"a selector never measures anything, so it takes no probe; to have members chosen by probing, put the selector's members in a %s or %s group and choose that",
			blueprint.GroupTypeURLTest, blueprint.GroupTypeFallback)
	} else if settings.Probe != nil {
		group.Probe = blueprint.Probe{
			URL:              settings.Probe.URL,
			Interval:         durationOrNil(settings.Probe.Interval),
			Timeout:          durationOrNil(settings.Probe.Timeout),
			ExpectedStatuses: settings.Probe.ExpectedStatus.Ranges(),
		}
	}
	return group
}

func hasSettings(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) && !bytes.Equal(trimmed, []byte("null"))
}

func (b *configBuilder) buildFreedom(raw json.RawMessage, loc string, transport []rawModule, transportLoc string) *blueprint.Freedom {
	var settings rawFreedomSettings
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if b.strict {
			decoder.DisallowUnknownFields()
		}
		if err := decoder.Decode(&settings); err != nil {
			b.diagnostics.report(loc, err)
			return nil
		}
	}
	if settings.MovedTCP != nil {
		b.diagnostics.errorf(fieldLocation(loc, "tcp"),
			`freedom no longer takes tcp here. Put tcp's settings in its transport: [{"type": "tcp", "settings": {...}}]`)
	}

	freedom := &blueprint.Freedom{StreamTransport: b.buildFreedomStreamTransport(transport, transportLoc)}

	if newModule, err := b.modules.Transports().LookupBaseDatagram("udp"); err != nil {
		b.diagnostics.report(loc, err)
	} else if udp := newModule(); b.decodeSettings(udp, settings.UDP, fieldLocation(loc, "udp")) {
		freedom.DatagramTransport = blueprint.DatagramTransportStack{Base: udp}
	}

	if settings.NAT != nil {
		freedom.NAT = b.buildNATPolicy(*settings.NAT, fieldLocation(loc, "nat"))
		freedom.NATMaxDatagramSize = settings.NAT.MaxDatagramSize
	}
	return freedom
}

// Freedom connects straight to each destination, which runs none of our
// layers: its stack is tcp, then only overlays that need no server side. A
// refused layer is reported and left out, so Check does not report it again.
func (b *configBuilder) buildFreedomStreamTransport(stack []rawModule, loc string) blueprint.StreamTransportStack {
	transports := b.modules.Transports()
	if len(stack) == 0 {
		stack = []rawModule{{Type: freedomBaseTransport}}
	}

	var built blueprint.StreamTransportStack
	baseLoc := indexLocation(loc, 0)
	switch base := stack[0].Type; {
	case base == freedomBaseTransport:
		built = b.buildStreamTransportStack(stack[:1], loc)
	case isRegistered(transports.LookupOverlayStream(base)):
		b.diagnostics.errorf(baseLoc, `%q is an overlay and needs a connection below it to wrap. Put %q first: [{"type": %q}, {"type": %q}]`,
			base, freedomBaseTransport, freedomBaseTransport, base)
	case isRegistered(transports.LookupBaseDatagram(base)):
		b.diagnostics.errorf(baseLoc, "freedom's transport must start with %q, the base that connects straight to each destination; %q cannot carry freedom's TCP traffic. Freedom's UDP is set in its settings.udp",
			freedomBaseTransport, base)
	case isRegistered(transports.LookupBaseStream(base)):
		b.diagnostics.errorf(baseLoc, "freedom's transport must start with %q, the base that connects straight to each destination; got %q",
			freedomBaseTransport, base)
	default:
		_, err := transports.LookupBaseStream(base)
		b.diagnostics.report(baseLoc, err)
	}

	for index, layer := range stack[1:] {
		layerLoc := indexLocation(loc, index+1)
		overlay := b.buildStreamOverlay(layer, layerLoc)
		if overlay == nil {
			continue
		}
		if !needsNoServerSide(overlay) {
			b.diagnostics.errorf(layerLoc, "%q cannot be used with freedom. Freedom connects straight to each destination, and a destination does not run %q, so it would receive %s framing instead of the client's traffic. Transports that work with freedom: %s",
				layer.Type, layer.Type, layer.Type, strings.Join(b.transportsThatWorkWithFreedom(), ", "))
			continue
		}
		built.Overlays = append(built.Overlays, overlay)
	}
	return built
}

// Read from the registry, so a program's own modules are listed too.
func (b *configBuilder) transportsThatWorkWithFreedom() []string {
	names := []string{freedomBaseTransport}
	transports := b.modules.Transports()
	for _, listing := range transports.Modules() {
		if listing.Kind != registry.KindOverlayStream {
			continue
		}
		if newModule, err := transports.LookupOverlayStream(listing.Name); err == nil && needsNoServerSide(newModule()) {
			names = append(names, listing.Name)
		}
	}
	return names
}

func isRegistered[Constructor any](_ Constructor, err error) bool { return err == nil }

// Starts from nat's own default so an unset field keeps that package's
// choice; outbound decides nothing here. Each field is checked separately so a
// bad config file gets a diagnostic pointing at the field, not at the block.
func (b *configBuilder) buildNATPolicy(raw rawNAT, loc string) *nat.Policy {
	defaults := nat.NewDefaultPolicy()
	mapping, filtering, idleTimeout := defaults.Mapping(), defaults.Filtering(), defaults.IdleTimeout()

	if raw.Mapping != "" {
		if candidate := nat.MappingBehavior(raw.Mapping); candidate.Valid() {
			mapping = candidate
		} else {
			b.diagnostics.errorf(fieldLocation(loc, "mapping"),
				"unknown mapping behavior %q; want endpoint-independent, address-dependent or address-and-port-dependent", raw.Mapping)
		}
	}

	if raw.Filtering != "" {
		if candidate := nat.FilteringBehavior(raw.Filtering); candidate.Valid() {
			filtering = candidate
		} else {
			b.diagnostics.errorf(fieldLocation(loc, "filtering"),
				"unknown filtering behavior %q; want endpoint-independent, address-dependent or address-and-port-dependent", raw.Filtering)
		}
	}

	if raw.IdleTimeout != nil {
		if candidate := time.Duration(*raw.IdleTimeout); candidate > 0 {
			idleTimeout = candidate
		} else {
			b.diagnostics.errorf(fieldLocation(loc, "idle_timeout"), "idle_timeout must be greater than zero; leave it out for the default (%s)", defaults.IdleTimeout())
		}
	}

	policy, err := nat.NewPolicy(mapping, filtering, idleTimeout)
	if err != nil {
		b.diagnostics.report(loc, err)
		return nil
	}
	return &policy
}
