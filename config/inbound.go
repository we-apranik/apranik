package config

import (
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/spec"
)

func (b *configBuilder) buildInbound(raw rawInbound, loc string) blueprint.Inbound {
	inbound := blueprint.Inbound{
		Tag:                   raw.Tag,
		Network:               b.parseNetwork(raw.Network, fieldLocation(loc, "network")),
		HandshakeTimeout:      time.Duration(raw.HandshakeTimeout),
		TCPIdleTimeout:        time.Duration(raw.TCPIdleTimeout),
		UDPSessionIdleTimeout: time.Duration(raw.UDPSessionIdleTimeout),
		MaxConnections:        raw.MaxConnections,
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

	if raw.Sniffing != nil {
		inbound.Sniffing = b.buildSniffing(*raw.Sniffing, fieldLocation(loc, "sniffing"))
	}

	return inbound
}

func (b *configBuilder) buildSniffing(raw rawSniffing, loc string) *blueprint.Sniffing {
	sniffing := &blueprint.Sniffing{Timeout: time.Duration(raw.Timeout)}

	for index, rawSniffer := range raw.Sniffers {
		snifferLoc := indexLocation(fieldLocation(loc, "sniffers"), index)
		newModule, err := b.modules.Sniffers().Lookup(rawSniffer.Type)
		if err != nil {
			b.diagnostics.report(snifferLoc, err)
			continue
		}
		sniffer := newModule()
		if b.decodeSettings(sniffer, rawSniffer.Settings, fieldLocation(snifferLoc, "settings")) {
			sniffing.Sniffers = append(sniffing.Sniffers, sniffer)
		}
	}
	return sniffing
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
