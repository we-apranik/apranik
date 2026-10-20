package config

import (
	"bytes"
	"encoding/json"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/registry"
	"github.com/we-apranik/apranik/spec"
)

type rawDNS struct {
	IPVersions    string         `json:"ip_versions"`
	Cache         rawDNSCache    `json:"cache"`
	Servers       []rawDNSServer `json:"servers"`
	Rules         []rawDNSRule   `json:"rules"`
	DefaultServer string         `json:"default_server"`
}

type rawDNSCache struct {
	Disable    bool           `json:"disable"`
	MaxEntries *int           `json:"max_entries"`
	PerServer  bool           `json:"per_server"`
	ServeStale *rawServeStale `json:"serve_stale"`
}

type rawServeStale struct {
	Enabled     bool            `json:"enabled"`
	MaxStaleAge *types.Duration `json:"max_stale_age"`
}

// type and settings are the module's; the rest are core's, which is why they sit beside settings and not in it.
type rawDNSServer struct {
	Tag        string          `json:"tag"`
	Type       string          `json:"type"`
	Settings   json.RawMessage `json:"settings"`
	TLS        json.RawMessage `json:"tls"`
	Outbound   string          `json:"outbound"`
	IPVersions string          `json:"ip_versions"`
	DNSServer  string          `json:"dns_server"`
}

type rawSystemDNSSettings struct {
	Timeout *types.Duration `json:"timeout"`
}

type rawDNSRule struct {
	Match  rawMatch `json:"match"`
	Server string   `json:"server"`
}

const dnsTLSTransport = "tls"

func (b *configBuilder) buildDNS(raw rawDNS, loc string) *blueprint.DNS {
	dns := &blueprint.DNS{
		IPVersions:    blueprint.IPVersions(raw.IPVersions),
		DefaultServer: raw.DefaultServer,
		Cache: blueprint.DNSCache{
			Disable:    raw.Cache.Disable,
			MaxEntries: raw.Cache.MaxEntries,
			PerServer:  raw.Cache.PerServer,
		},
	}
	if raw.Cache.ServeStale != nil {
		dns.Cache.ServeStale = raw.Cache.ServeStale.Enabled
		dns.Cache.MaxStaleAge = durationOrNil(raw.Cache.ServeStale.MaxStaleAge)
	}
	for index, server := range raw.Servers {
		dns.Servers = append(dns.Servers, b.buildDNSServer(server, indexLocation(fieldLocation(loc, "servers"), index)))
	}
	for index, rule := range raw.Rules {
		ruleLoc := indexLocation(fieldLocation(loc, "rules"), index)
		dns.Rules = append(dns.Rules, blueprint.DNSRule{
			Match:  b.buildMatch(rule.Match, fieldLocation(ruleLoc, "match")),
			Server: rule.Server,
		})
	}
	return dns
}

func (b *configBuilder) buildDNSServer(raw rawDNSServer, loc string) blueprint.DNSServer {
	server := blueprint.DNSServer{
		Tag:        raw.Tag,
		Outbound:   raw.Outbound,
		IPVersions: blueprint.IPVersions(raw.IPVersions),
		DNSServer:  raw.DNSServer,
	}
	settingsLoc := fieldLocation(loc, "settings")
	tlsLoc := fieldLocation(loc, "tls")
	dnsServers := b.modules.DNSServers()

	switch kinds := dnsServers.Kinds(raw.Type); {
	case raw.Type == "":
		b.diagnostics.report(loc, errMissingField("type", "dns server"))
		return server
	case raw.Type == registry.SystemDNSServerName:
		var settings rawSystemDNSSettings
		if b.decodeStrict(raw.Settings, &settings, settingsLoc) {
			server.System = &blueprint.SystemDNSServer{Timeout: durationOrNil(settings.Timeout)}
		}
	case len(kinds) == 1 && kinds[0] == registry.KindDatagramDNSServer:
		newModule, _ := dnsServers.LookupDatagram(raw.Type)
		if module := newModule(); b.decodeSettings(module, raw.Settings, settingsLoc) {
			server.Datagram = module
		}
	default:
		newModule, err := dnsServers.LookupStream(raw.Type)
		if err != nil {
			b.diagnostics.report(fieldLocation(loc, "type"), err)
			return server
		}
		if module := newModule(); b.decodeSettings(module, raw.Settings, settingsLoc) {
			server.Stream = module
		}
	}

	hasTLSBlock := raw.TLS != nil && string(raw.TLS) != "null"
	switch {
	case server.Stream != nil && server.Stream.NeedsTLS():
		server.TLS = b.buildDNSServerTLS(withApplicationProtocolsTheModuleSpeaks(raw.TLS, server.Stream), tlsLoc)
	case hasTLSBlock && (server.Stream != nil || server.Datagram != nil || server.System != nil):
		b.diagnostics.errorf(tlsLoc, "dns server type %q does not run over TLS, so it takes no tls; dot and doh do", raw.Type)
	}
	return server
}

func (b *configBuilder) buildDNSServerTLS(raw json.RawMessage, loc string) spec.OverlayStreamTransportModule {
	newModule, err := b.modules.Transports().LookupOverlayStream(dnsTLSTransport)
	if err != nil {
		b.diagnostics.report(loc, err)
		return nil
	}
	overlay := newModule()
	if !b.decodeSettings(overlay, raw, loc) {
		return nil
	}
	return overlay
}

func withApplicationProtocolsTheModuleSpeaks(rawTLS json.RawMessage, module spec.StreamDNSServerModule) json.RawMessage {
	provider, provides := module.(spec.TransportInfoProvider)
	if !provides || len(provider.TransportInfo().ApplicationProtocols) == 0 {
		return rawTLS
	}
	fields := map[string]json.RawMessage{}
	if trimmed := bytes.TrimSpace(rawTLS); len(trimmed) > 0 && string(trimmed) != "null" {
		if json.Unmarshal(trimmed, &fields) != nil {
			return rawTLS
		}
	}
	if _, written := fields["alpn"]; written {
		return rawTLS
	}
	protocols, err := json.Marshal(provider.TransportInfo().ApplicationProtocols)
	if err != nil {
		return rawTLS
	}
	fields["alpn"] = protocols
	merged, err := json.Marshal(fields)
	if err != nil {
		return rawTLS
	}
	return merged
}

func dnsServerModule(server blueprint.DNSServer) spec.Config {
	switch {
	case server.Stream != nil:
		return server.Stream
	case server.Datagram != nil:
		return server.Datagram
	}
	return nil
}
