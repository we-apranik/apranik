package config

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/core/registry"
)

const (
	freedomProtocol = registry.FreedomProtocolName
	blockProtocol   = registry.BlockProtocolName
)

func (b *configBuilder) buildOutbound(raw rawOutbound, loc string) blueprint.Outbound {
	outbound := blueprint.Outbound{
		Tag:              raw.Tag,
		DialTimeout:      time.Duration(raw.DialTimeout),
		HandshakeTimeout: time.Duration(raw.HandshakeTimeout),
	}

	if raw.Server != "" {
		server, err := parseServerAddr(raw.Server, raw.ServerPort)
		if err != nil {
			b.diagnostics.report(fieldLocation(loc, "server"), err)
		} else {
			outbound.Server = server
		}
	}

	if len(raw.Transport) > 0 {
		outbound.StreamTransport = b.buildStreamTransportStack(raw.Transport, fieldLocation(loc, "transport"))
	}

	protocolLoc := fieldLocation(loc, "protocol")
	switch raw.Protocol.Type {
	case "":
		b.diagnostics.report(protocolLoc, errMissingField("type", "protocol"))
	case freedomProtocol:
		outbound.Freedom = b.buildFreedom(raw.Protocol.Settings, fieldLocation(protocolLoc, "settings"))
	case blockProtocol:
		if hasSettings(raw.Protocol.Settings) {
			b.diagnostics.errorf(fieldLocation(protocolLoc, "settings"), "block takes no settings")
		}
		outbound.Block = true
	default:
		outbound.Proxy = b.buildStreamProxy(raw.Protocol, protocolLoc)
	}

	return outbound
}

func hasSettings(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) && !bytes.Equal(trimmed, []byte("null"))
}

func (b *configBuilder) buildFreedom(raw json.RawMessage, loc string) *blueprint.Freedom {
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

	freedom := &blueprint.Freedom{}
	transports := b.modules.Transports()

	if newModule, err := transports.LookupBaseStream("tcp"); err != nil {
		b.diagnostics.report(loc, err)
	} else if tcp := newModule(); b.decodeSettings(tcp, settings.TCP, fieldLocation(loc, "tcp")) {
		freedom.StreamTransport = blueprint.StreamTransportStack{Base: tcp}
	}

	if newModule, err := transports.LookupBaseDatagram("udp"); err != nil {
		b.diagnostics.report(loc, err)
	} else if udp := newModule(); b.decodeSettings(udp, settings.UDP, fieldLocation(loc, "udp")) {
		freedom.DatagramTransport = blueprint.DatagramTransportStack{Base: udp}
	}

	if settings.NAT != nil {
		freedom.NAT = b.buildNATPolicy(*settings.NAT, fieldLocation(loc, "nat"))
	}
	return freedom
}

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

	if raw.IdleTimeout != 0 {
		if candidate := time.Duration(raw.IdleTimeout); candidate > 0 {
			idleTimeout = candidate
		} else {
			b.diagnostics.errorf(fieldLocation(loc, "idle_timeout"), "idle_timeout must be positive")
		}
	}

	policy, err := nat.NewPolicy(mapping, filtering, idleTimeout)
	if err != nil {
		b.diagnostics.report(loc, err)
		return nil
	}
	return &policy
}
