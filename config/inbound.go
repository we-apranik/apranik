package config

import (
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/spec"
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
		inbound.DatagramProxy = b.buildDatagramProxy(raw.Protocol, protocolLoc)
	default:
		inbound.StreamTransport = b.buildStreamTransportStack(raw.Transport, transportLoc)
		if raw.Protocol.Type == mixedProtocol {
			inbound.Mixed = b.buildMixed(raw.Protocol.Settings, fieldLocation(protocolLoc, "settings"))
			break
		}
		inbound.StreamProxy = b.buildStreamProxy(raw.Protocol, protocolLoc)
		if inbound.StreamProxy != nil {
			inbound.DatagramOverStreamProxy = b.buildDatagramOverStreamProxy(raw.Protocol, protocolLoc)
		}
	}

	if raw.Fallback != nil {
		fallback, err := parseServerAddr(raw.Fallback.Server, raw.Fallback.ServerPort)
		if err != nil {
			b.diagnostics.report(fieldLocation(loc, "fallback", "server"), err)
		} else {
			inbound.Fallback = fallback
		}
	}

	if raw.MovedSniffing != nil {
		b.diagnostics.errorf(fieldLocation(loc, "sniffing"),
			`sniffing moved to a routing rule: {"match": {"inbound": [%q]}, "action": {"type": "sniff", "settings": {"sniffers": [...]}}}`, raw.Tag)
	}

	if raw.MovedMaxConnections != nil {
		b.diagnostics.errorf(fieldLocation(loc, "max_connections"),
			"max_connections was renamed max_tcp_connections; UDP sessions have their own limit, max_udp_sessions")
	}

	if raw.MovedHandshakeTimeout != nil {
		b.diagnostics.errorf(fieldLocation(loc, "handshake_timeout"),
			`handshake_timeout moved into each layer's own settings: {"type": "tls", "settings": {"handshake_timeout": "10s"}}, and likewise for every transport and protocol`)
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
