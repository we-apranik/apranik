package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/spec"
)

func dnsServerTags(section *blueprint.DNS) map[string]bool {
	if section == nil {
		return map[string]bool{dns.SystemServerTag: true}
	}
	tags := make(map[string]bool, len(section.Servers))
	for _, server := range section.Servers {
		tags[server.Tag] = true
	}
	return tags
}

func checkDNS(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint, datasetKinds map[string]blueprint.DatasetKind) {
	outbounds := make(map[string]blueprint.Outbound, len(coreBlueprint.Outbounds))
	for _, outbound := range coreBlueprint.Outbounds {
		outbounds[outbound.Tag] = outbound
	}
	serverTags := dnsServerTags(coreBlueprint.DNS)

	for index, outbound := range coreBlueprint.Outbounds {
		if outbound.DNSServer != "" && !serverTags[outbound.DNSServer] {
			diagnostics.errorf(fieldLocation(indexLocation("outbounds", index), "dns_server"), "no dns server is tagged %q", outbound.DNSServer)
		}
	}

	section := coreBlueprint.DNS
	if section == nil {
		return
	}
	if section.IPVersions != "" && !section.IPVersions.Valid() {
		diagnostics.errorf(fieldLocation("dns", "ip_versions"), "unknown ip_versions %q; want %s", section.IPVersions, ipVersionsList())
	}
	checkDNSCache(diagnostics, section.Cache)

	if len(section.Servers) == 0 {
		diagnostics.errorf(fieldLocation("dns", "servers"), "a dns section needs at least one server; without the section, the operating system resolves")
	}
	seen := make(map[string]int, len(section.Servers))
	for index, server := range section.Servers {
		loc := indexLocation(fieldLocation("dns", "servers"), index)
		if server.Tag == "" {
			diagnostics.errorf(fieldLocation(loc, "tag"), "a dns server needs a tag")
		} else if first, duplicate := seen[server.Tag]; duplicate {
			diagnostics.errorf(fieldLocation(loc, "tag"), "tag %q is already used by dns.servers[%d]", server.Tag, first)
		} else {
			seen[server.Tag] = index
		}
		checkDNSServer(diagnostics, server, outbounds, serverTags, loc)
	}

	for index, rule := range section.Rules {
		ruleLoc := indexLocation(fieldLocation("dns", "rules"), index)
		matchLoc := fieldLocation(ruleLoc, "match")
		if dns.HasNonDomainField(rule.Match) || anyNestedMatch(rule.Match, dns.HasNonDomainField) {
			diagnostics.errorf(matchLoc, "a dns rule matches only on the name: domain, domain_suffix, domain_keyword, domain_regex, domain_in_dataset, any, all and not")
		}
		checkDNSRuleDatasets(diagnostics, datasetKinds, rule.Match, matchLoc)
		if isEmptyMatch(rule.Match) && index < len(section.Rules)-1 {
			diagnostics.errorf(indexLocation(fieldLocation("dns", "rules"), index+1),
				"this rule and every rule after it can never run: dns.rules[%d] matches every name", index)
		}
		switch {
		case rule.Server == "":
			diagnostics.report(ruleLoc, errMissingField("server", "dns rule"))
		case !serverTags[rule.Server]:
			diagnostics.errorf(fieldLocation(ruleLoc, "server"), "no dns server is tagged %q", rule.Server)
		}
	}

	switch {
	case section.DefaultServer == "":
		diagnostics.errorf(fieldLocation("dns", "default_server"), "the dns section needs a default_server, which answers every name no rule matches")
	case !serverTags[section.DefaultServer]:
		diagnostics.errorf(fieldLocation("dns", "default_server"), "no dns server is tagged %q", section.DefaultServer)
	}

	checkDNSLoops(diagnostics, coreBlueprint)
	warnAboutDirectServersBesideAProxy(diagnostics, coreBlueprint, outbounds)
}

func ipVersionsList() string {
	return quotedList([]string{
		string(blueprint.IPVersionsIPv4Only), string(blueprint.IPVersionsIPv6Only),
		string(blueprint.IPVersionsPreferIPv4), string(blueprint.IPVersionsPreferIPv6),
	})
}

func checkDNSCache(diagnostics *Diagnostics, cache blueprint.DNSCache) {
	loc := fieldLocation("dns", "cache")
	if cache.MaxEntries != nil && *cache.MaxEntries <= 0 {
		diagnostics.errorf(fieldLocation(loc, "max_entries"), "must be greater than zero (leave it out for the default, %d)", dns.DefaultCacheMaxEntries)
	}
	if cache.MaxStaleAge != nil && *cache.MaxStaleAge <= 0 {
		diagnostics.errorf(fieldLocation(loc, "serve_stale", "max_stale_age"), "must be greater than zero (leave it out for the default, %s)", dns.DefaultMaxStaleAge)
	}
	if cache.ServeStale && cache.Disable {
		diagnostics.errorf(fieldLocation(loc, "serve_stale"), "serving stale answers needs the cache, and the cache is disabled")
	}
	if cache.Disable && cache.PerServer {
		diagnostics.warnf(fieldLocation(loc, "per_server"), "the cache is disabled, so per_server does nothing")
	}
}

func checkDNSServer(diagnostics *Diagnostics, server blueprint.DNSServer, outbounds map[string]blueprint.Outbound, serverTags map[string]bool, loc string) {
	kinds := 0
	for _, set := range []bool{server.Stream != nil, server.Datagram != nil, server.System != nil} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		if kinds > 1 {
			diagnostics.errorf(fieldLocation(loc, "type"), "a dns server is exactly one type")
		}
		return
	}

	if server.IPVersions != "" && !server.IPVersions.Valid() {
		diagnostics.errorf(fieldLocation(loc, "ip_versions"), "unknown ip_versions %q; want %s", server.IPVersions, ipVersionsList())
	}
	if server.DNSServer != "" && !serverTags[server.DNSServer] {
		diagnostics.errorf(fieldLocation(loc, "dns_server"), "no dns server is tagged %q", server.DNSServer)
	}

	if server.System != nil {
		if server.Outbound != "" {
			diagnostics.errorf(fieldLocation(loc, "outbound"), "system asks the operating system, which does its own dialing; it takes no outbound")
		}
		if server.DNSServer != "" {
			diagnostics.errorf(fieldLocation(loc, "dns_server"), "system has no hostname of its own to resolve")
		}
		if server.System.Timeout != nil && *server.System.Timeout <= 0 {
			diagnostics.errorf(fieldLocation(loc, "settings", "timeout"), "must be greater than zero (leave it out for the default, %s)", dns.DefaultSystemTimeout)
		}
		return
	}

	collectWarnings(diagnostics, dnsServerModule(server), fieldLocation(loc, "settings"))
	if server.TLS != nil {
		collectWarnings(diagnostics, server.TLS, fieldLocation(loc, "tls"))
	}
	if server.Stream != nil && server.TLS != nil {
		checkDNSServerApplicationProtocols(diagnostics, server, loc)
	}

	if server.Outbound == "" {
		return
	}
	outboundLoc := fieldLocation(loc, "outbound")
	if server.Outbound == blueprint.DefaultDNSOutboundTag {
		return
	}
	outbound, found := outbounds[server.Outbound]
	if !found {
		diagnostics.errorf(outboundLoc, "no outbound is tagged %q", server.Outbound)
		return
	}
	if server.Datagram != nil {
		if !carriesUDP(outbound, outbounds) {
			diagnostics.errorf(outboundLoc, "dns server type %q sends datagrams, and outbound %q cannot carry UDP", server.Datagram.Info().Name, outbound.Tag)
		} else if outbound.Freedom != nil && len(outbound.Freedom.StreamTransport.Overlays) > 0 {
			diagnostics.warnf(outboundLoc, "outbound %q's transport only shapes TCP; these UDP queries go out without it", outbound.Tag)
		}
	}
}

func checkDNSServerApplicationProtocols(diagnostics *Diagnostics, server blueprint.DNSServer, loc string) {
	offered := transportApplicationProtocols(server.TLS)
	provider, provides := server.Stream.(spec.TransportInfoProvider)
	if len(offered) == 0 || !provides {
		return
	}
	speakable := provider.TransportInfo().ApplicationProtocols
	if len(speakable) == 0 {
		return
	}
	for _, protocol := range offered {
		if !slices.Contains(speakable, protocol) {
			diagnostics.errorf(fieldLocation(loc, "tls", "alpn"),
				"tls offers %v, but %s can speak only %v, and the server may choose %s; set alpn to %v",
				offered, server.Stream.Info().Name, speakable, protocol, quotedList(speakable))
			return
		}
	}
}

func checkDNSRuleDatasets(diagnostics *Diagnostics, datasetKinds map[string]blueprint.DatasetKind, fields blueprint.Match, loc string) {
	checkDatasetReferences(diagnostics, datasetKinds, fields.DomainDatasets, blueprint.DatasetKindDomain, fieldLocation(loc, "domain_in_dataset"))
	for index, alternative := range fields.Any {
		checkDNSRuleDatasets(diagnostics, datasetKinds, alternative, indexLocation(fieldLocation(loc, "any"), index))
	}
	for index, required := range fields.All {
		checkDNSRuleDatasets(diagnostics, datasetKinds, required, indexLocation(fieldLocation(loc, "all"), index))
	}
	if fields.Not != nil {
		checkDNSRuleDatasets(diagnostics, datasetKinds, *fields.Not, fieldLocation(loc, "not"))
	}
}

func checkDNSLoops(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	section := coreBlueprint.DNS
	type node struct{ kind, tag string }
	edges := map[node][]node{}
	outboundIndex := map[string]int{}
	for index, outbound := range coreBlueprint.Outbounds {
		outboundIndex[outbound.Tag] = index
		if outbound.Group != nil {
			for _, member := range outbound.Group.Members {
				edges[node{"outbound", outbound.Tag}] = append(edges[node{"outbound", outbound.Tag}], node{"outbound", member})
			}
			continue
		}
		if outbound.Proxy == nil {
			continue
		}
		if _, isDomain := outbound.Server.Domain(); !isDomain {
			continue
		}
		resolvedBy := outbound.DNSServer
		if resolvedBy == "" {
			resolvedBy = section.DefaultServer
		}
		edges[node{"outbound", outbound.Tag}] = append(edges[node{"outbound", outbound.Tag}], node{"dns server", resolvedBy})
	}
	serverIndex := map[string]int{}
	for index, server := range section.Servers {
		serverIndex[server.Tag] = index
		from := node{"dns server", server.Tag}
		if server.Outbound != "" {
			edges[from] = append(edges[from], node{"outbound", server.Outbound})
		}
		if server.DNSServer != "" {
			edges[from] = append(edges[from], node{"dns server", server.DNSServer})
		}
	}

	const (
		unvisited = iota
		onPath
		finished
	)
	state := map[node]int{}
	var path []node
	reported := map[string]bool{}
	var visit func(current node)
	visit = func(current node) {
		state[current] = onPath
		path = append(path, current)
		for _, next := range edges[current] {
			switch state[next] {
			case onPath:
				start := slices.Index(path, next)
				if !slices.ContainsFunc(path[start:], func(step node) bool { return step.kind == "dns server" }) {
					// Groups waiting on each other: checkGroups says so.
					continue
				}
				names := make([]string, 0, len(path)-start+1)
				for _, step := range path[start:] {
					names = append(names, fmt.Sprintf("%s %q", step.kind, step.tag))
				}
				names = append(names, fmt.Sprintf("%s %q", next.kind, next.tag))
				description := strings.Join(names, " → ")
				if reported[loopKey(path[start:])] {
					continue
				}
				reported[loopKey(path[start:])] = true
				loc := indexLocation(fieldLocation("dns", "servers"), serverIndex[next.tag])
				if next.kind == "outbound" {
					loc = indexLocation("outbounds", outboundIndex[next.tag])
				}
				diagnostics.errorf(loc, "these lookups wait on each other: %s; give one of them a dns_server that does not go through the others", description)
			case unvisited:
				visit(next)
			}
		}
		path = path[:len(path)-1]
		state[current] = finished
	}
	for _, server := range section.Servers {
		if start := (node{"dns server", server.Tag}); state[start] == unvisited {
			visit(start)
		}
	}
	for _, outbound := range coreBlueprint.Outbounds {
		if start := (node{"outbound", outbound.Tag}); state[start] == unvisited {
			visit(start)
		}
	}
}

func loopKey[Node any](loop []Node) string {
	names := make([]string, len(loop))
	for index, step := range loop {
		names[index] = fmt.Sprint(step)
	}
	slices.Sort(names)
	return strings.Join(names, "|")
}

func warnAboutDirectServersBesideAProxy(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint, outbounds map[string]blueprint.Outbound) {
	var defaultOutbound blueprint.Outbound
	switch {
	case coreBlueprint.Routing != nil && coreBlueprint.Routing.DefaultAction.Route != nil:
		defaultOutbound = outbounds[coreBlueprint.Routing.DefaultAction.Route.Outbound]
	case coreBlueprint.Routing == nil && len(coreBlueprint.Outbounds) > 0:
		defaultOutbound = coreBlueprint.Outbounds[0]
	}
	if !routesThroughProxy(defaultOutbound, outbounds, map[string]bool{}) {
		return
	}
	for index, server := range coreBlueprint.DNS.Servers {
		if server.Outbound == "" && (server.Stream != nil || server.Datagram != nil) {
			diagnostics.warnf(fieldLocation(indexLocation(fieldLocation("dns", "servers"), index), "outbound"),
				"traffic goes through %q by default, but this dns server has no outbound, so its queries go out directly and show the network which names are looked up", defaultOutbound.Tag)
		}
	}
}

func routesThroughProxy(outbound blueprint.Outbound, outbounds map[string]blueprint.Outbound, visiting map[string]bool) bool {
	if outbound.Group == nil {
		return outbound.Proxy != nil
	}
	if visiting[outbound.Tag] {
		return false
	}
	visiting[outbound.Tag] = true
	defer delete(visiting, outbound.Tag)
	for _, tag := range outbound.Group.Members {
		if routesThroughProxy(outbounds[tag], outbounds, visiting) {
			return true
		}
	}
	return false
}
