package config

import (
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/registry"
	"github.com/we-apranik/apranik/spec"
)

func (b *configBuilder) buildInbound(raw rawInbound, loc string) blueprint.Inbound {
	inbound := blueprint.Inbound{
		Tag:                   raw.Tag,
		Network:               b.parseNetwork(raw.Network, fieldLocation(loc, "network")),
		TCPIdleTimeout:        durationOrNil(raw.TCPIdleTimeout),
		UDPSessionIdleTimeout: durationOrNil(raw.UDPSessionIdleTimeout),

		UDPMaxDatagramSize: raw.UDPMaxDatagramSize,

		UDPRoutingCacheEntryIdleTimeout:     durationOrNil(raw.UDPRoutingCacheEntryIdleTimeout),
		UDPRoutingCacheMaxEntriesPerSession: raw.UDPRoutingCacheMaxEntriesPerSession,
		MaxTCPConnections:                   raw.MaxTCPConnections,
		MaxUDPSessions:                      raw.MaxUDPSessions,
		MuxCool:                             buildMuxCool(raw.MuxCool),
	}

	if raw.Listen != "" {
		bind, err := parseListenAddr(raw.Listen, raw.Port)
		if err != nil {
			b.diagnostics.report(fieldLocation(loc, "listen"), err)
		} else {
			inbound.Bind = bind
		}
	}

	transportLoc := fieldLocation(loc, "transport")
	protocolLoc := fieldLocation(loc, "protocol")

	switch inbound.Network {
	case spec.NetworkUDP:
		inbound.DatagramTransport = b.buildDatagramTransportStack(raw.Transport, transportLoc)
		if raw.Protocol.Type == mixedProtocol {
			b.diagnostics.errorf(fieldLocation(protocolLoc, "type"), "mixed serves SOCKS5 and HTTP over streams; a udp inbound cannot use it")
			break
		}
		inbound.DatagramProxy = b.buildProxyModules(raw.Protocol, protocolLoc, registry.KindDatagramProxy).Datagram
	default:
		inbound.StreamTransport = b.buildStreamTransportStack(raw.Transport, transportLoc)
		if raw.Protocol.Type == mixedProtocol {
			inbound.Mixed = b.buildMixed(raw.Protocol.Settings, fieldLocation(protocolLoc, "settings"))
			break
		}
		modules := b.buildProxyModules(raw.Protocol, protocolLoc, registry.KindStreamProxy)
		inbound.StreamProxy = modules.Stream
		inbound.DatagramOverStreamProxy = modules.DatagramOverStream
	}

	if raw.Fallback != nil {
		fallback, err := parseServerAddr(raw.Fallback.Server, raw.Fallback.ServerPort)
		if err != nil {
			b.diagnostics.report(fieldLocation(loc, "fallback", "server"), err)
		} else {
			inbound.Fallback = fallback
		}
	}

	return inbound
}

// Never inferred from the base transport: inference breaks the moment one
// module registers as both base-stream and base-datagram, as QUIC would.
func (b *configBuilder) parseNetwork(name, loc string) spec.Network {
	switch name {
	case "", string(spec.NetworkTCP):
		return spec.NetworkTCP
	case string(spec.NetworkUDP):
		return spec.NetworkUDP
	default:
		b.diagnostics.errorf(loc, "unknown network %q; want \"tcp\" or \"udp\"", name)
		return spec.NetworkTCP
	}
}
