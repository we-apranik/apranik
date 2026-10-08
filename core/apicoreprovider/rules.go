package apicoreprovider

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/spec"
)

// RoutingRules lists the routing section's rules in the order they are tried,
// then the default action. Hits come from the statistics and are marked as not
// counted when there are none.
func (p *Provider) RoutingRules() ([]spec.APIRoutingRule, error) {
	var hits map[string]uint64
	if p.stats != nil {
		hits = p.stats.RuleHits()
	}
	hitsFor := func(location string) (uint64, bool) {
		if p.stats == nil {
			return 0, false
		}
		return hits[location], true
	}

	routing := p.blueprint.Routing
	var rules []spec.APIRoutingRule
	if routing != nil {
		rules = make([]spec.APIRoutingRule, 0, len(routing.Rules)+1)
		for index, rule := range routing.Rules {
			location := router.RuleLocation(index)
			listed := describeAction(rule.Action)
			listed.Location = location
			listed.Match = describeMatch(rule.Match)
			listed.Hits, listed.HitsCounted = hitsFor(location)
			rules = append(rules, listed)
		}
	}

	defaultRule := spec.APIRoutingRule{Location: router.DefaultRuleLocation}
	if tag := p.DefaultOutboundTag(); tag != "" {
		defaultRule.Action = spec.APIRoutingRuleActionRoute
		defaultRule.OutboundTag = tag
	} else {
		defaultRule.Action = spec.APIRoutingRuleActionBlock
	}
	defaultRule.Hits, defaultRule.HitsCounted = hitsFor(router.DefaultRuleLocation)
	return append(rules, defaultRule), nil
}

func describeAction(action blueprint.Action) spec.APIRoutingRule {
	switch {
	case action.Route != nil:
		return spec.APIRoutingRule{Action: spec.APIRoutingRuleActionRoute, OutboundTag: action.Route.Outbound}
	case action.Block:
		return spec.APIRoutingRule{Action: spec.APIRoutingRuleActionBlock}
	case action.Sniff != nil:
		return spec.APIRoutingRule{Action: spec.APIRoutingRuleActionSniff}
	default:
		resolve := action.Resolve
		if resolve == nil {
			return spec.APIRoutingRule{}
		}
		return spec.APIRoutingRule{Action: spec.APIRoutingRuleActionResolve, DNSServerTag: resolve.Server}
	}
}

// describeMatch writes a match as the fields of the configuration file with
// their values: "domain_suffix=a.com,b.com port=443". Fields all hold and a
// field's values are alternatives; any(...) and all(...) hold nested matches
// separated by " | ", not(...) holds one. A match with no condition is "*".
func describeMatch(match blueprint.Match) string {
	var fields []string
	add := func(name string, values ...string) {
		if len(values) > 0 {
			fields = append(fields, name+"="+strings.Join(values, ","))
		}
	}

	add("inbound", match.Inbounds...)
	if match.Network != "" {
		add("network", string(match.Network))
	}
	add("user", match.Users...)
	add("sniffed_protocol", match.SniffedProtocols...)
	add("sniffed_request_header", describeHeaders(match)...)
	add("sniffed_browser_family", match.SniffedBrowserFamilies...)
	add("domain", match.Domains...)
	add("domain_suffix", match.DomainSuffixes...)
	add("domain_keyword", match.DomainKeywords...)
	var regexes []string
	for _, expression := range match.DomainRegexes {
		regexes = append(regexes, expression.String())
	}
	add("domain_regex", regexes...)
	add("ip", describePrefixes(match.IPs)...)
	if match.IPIsPrivate != nil {
		add("ip_is_private", strconv.FormatBool(*match.IPIsPrivate))
	}
	var versions []string
	for _, version := range match.IPVersions {
		versions = append(versions, strconv.Itoa(version))
	}
	add("ip_version", versions...)
	add("port", describePorts(match.Ports)...)
	add("source_ip", describePrefixes(match.SourceIPs)...)
	if match.SourceIPIsPrivate != nil {
		add("source_ip_is_private", strconv.FormatBool(*match.SourceIPIsPrivate))
	}
	add("source_port", describePorts(match.SourcePorts)...)
	add("inbound_local_ip", describePrefixes(match.InboundLocalIPs)...)
	add("inbound_local_port", describePorts(match.InboundLocalPorts)...)
	add("domain_in_dataset", match.DomainDatasets...)
	add("ip_in_dataset", match.IPDatasets...)
	add("source_ip_in_dataset", match.SourceIPDatasets...)

	if len(match.Any) > 0 {
		fields = append(fields, "any("+describeAlternatives(match.Any)+")")
	}
	if len(match.All) > 0 {
		fields = append(fields, "all("+describeAlternatives(match.All)+")")
	}
	if match.Not != nil {
		fields = append(fields, "not("+describeMatch(*match.Not)+")")
	}
	if len(fields) == 0 {
		return "*"
	}
	return strings.Join(fields, " ")
}

func describeAlternatives(matches []blueprint.Match) string {
	described := make([]string, len(matches))
	for index, nested := range matches {
		described[index] = describeMatch(nested)
	}
	return strings.Join(described, " | ")
}

func describeHeaders(match blueprint.Match) []string {
	names := make([]string, 0, len(match.SniffedRequestHeaders))
	for name := range match.SniffedRequestHeaders {
		names = append(names, name)
	}
	slices.Sort(names)
	described := make([]string, len(names))
	for index, name := range names {
		described[index] = fmt.Sprintf("%s~%s", name, match.SniffedRequestHeaders[name])
	}
	return described
}

func describePrefixes(prefixes []netip.Prefix) []string {
	described := make([]string, len(prefixes))
	for index, prefix := range prefixes {
		if prefix.IsSingleIP() {
			described[index] = prefix.Addr().String()
		} else {
			described[index] = prefix.String()
		}
	}
	return described
}

func describePorts(ports []blueprint.PortRange) []string {
	described := make([]string, len(ports))
	for index, ports := range ports {
		if ports.First == ports.Last {
			described[index] = strconv.Itoa(int(ports.First))
		} else {
			described[index] = fmt.Sprintf("%d-%d", ports.First, ports.Last)
		}
	}
	return described
}
