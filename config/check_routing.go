package config

import (
	"slices"
	"strings"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/route/dataset"
	"github.com/we-apranik/apranik/spec"
)

const longSniffTimeout = 5 * time.Second

var knownBrowserFamilies = []string{spec.BrowserFamilyChromium, spec.BrowserFamilyFirefox, spec.BrowserFamilySafari}

func checkRouting(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint, datasetKinds map[string]blueprint.DatasetKind) {
	routing := coreBlueprint.Routing
	if routing == nil {
		if len(coreBlueprint.Outbounds) > 1 {
			diagnostics.warnf("routing", "there is no routing section, so every request goes to the first outbound, %q; the others are never used",
				coreBlueprint.Outbounds[0].Tag)
		}
		return
	}

	checker := routingChecker{
		diagnostics:  diagnostics,
		outbounds:    make(map[string]blueprint.Outbound, len(coreBlueprint.Outbounds)),
		inbounds:     make(map[string]bool, len(coreBlueprint.Inbounds)),
		datasetKinds: datasetKinds,
		dnsServers:   dnsServerTags(coreBlueprint.DNS),
	}
	for _, outbound := range coreBlueprint.Outbounds {
		checker.outbounds[outbound.Tag] = outbound
	}
	for _, inbound := range coreBlueprint.Inbounds {
		checker.inbounds[inbound.Tag] = true
	}

	sniffedBefore := false
	var pendingSniffLoc, pendingResolveLoc string
	for index, rule := range routing.Rules {
		ruleLoc := indexLocation(fieldLocation("routing", "rules"), index)
		matchLoc := fieldLocation(ruleLoc, "match")
		actionLoc := fieldLocation(ruleLoc, "action")

		checker.checkMatch(rule.Match, matchLoc)
		if !sniffedBefore && readsOnlySniffedFields(rule.Match) {
			diagnostics.warnf(matchLoc, "sniffed_protocol, sniffed_request_header or sniffed_browser_family is read before any sniff action, so it never matches")
		}
		if pendingSniffLoc != "" && readsSniffedData(rule.Match) {
			pendingSniffLoc = ""
		}
		if pendingResolveLoc != "" && readsDestinationIP(rule.Match) {
			pendingResolveLoc = ""
		}

		checker.checkAction(rule.Action, actionLoc, rule.Match)
		if rule.Action.Sniff != nil {
			sniffedBefore = true
			pendingSniffLoc = actionLoc
		}
		if rule.Action.Resolve != nil {
			pendingResolveLoc = actionLoc
		}

		if isEmptyMatch(rule.Match) && endsEvaluation(rule.Action) && index < len(routing.Rules)-1 {
			diagnostics.errorf(indexLocation(fieldLocation("routing", "rules"), index+1),
				"this rule and every rule after it can never run: routing.rules[%d] matches everything and decides", index)
		}
	}
	if pendingSniffLoc != "" {
		diagnostics.warnf(pendingSniffLoc, "no rule after this sniff reads a domain or a sniffed_* field, so sniffing only delays connections")
	}
	if pendingResolveLoc != "" {
		diagnostics.warnf(pendingResolveLoc, "no rule after this resolve reads an IP field, so resolving only delays connections")
	}

	defaultLoc := fieldLocation("routing", "default_action")
	switch action := routing.DefaultAction; {
	case action.Sniff != nil, action.Resolve != nil:
		diagnostics.errorf(defaultLoc, "default_action must route or block; sniff and resolve decide nothing")
	case action.Route == nil && !action.Block:
		diagnostics.errorf(defaultLoc, `routing needs a default_action, such as {"type": "route", "settings": {"outbound": "proxy"}}`)
	default:
		checker.checkAction(action, defaultLoc, blueprint.Match{})
	}
}

type routingChecker struct {
	diagnostics  *Diagnostics
	outbounds    map[string]blueprint.Outbound
	inbounds     map[string]bool
	datasetKinds map[string]blueprint.DatasetKind
	dnsServers   map[string]bool
}

func checkDatasets(diagnostics *Diagnostics, datasets []blueprint.Dataset) map[string]blueprint.DatasetKind {
	kinds := make(map[string]blueprint.DatasetKind, len(datasets))
	seen := make(map[string]int, len(datasets))
	for index, declaration := range datasets {
		loc := indexLocation("datasets", index)
		switch {
		case declaration.Tag == "":
			diagnostics.errorf(fieldLocation(loc, "tag"), "a dataset needs a tag")
		default:
			if first, duplicate := seen[declaration.Tag]; duplicate {
				diagnostics.errorf(fieldLocation(loc, "tag"), "tag %q is already used by datasets[%d]", declaration.Tag, first)
			} else {
				seen[declaration.Tag] = index
			}
		}
		if declaration.File == "" {
			diagnostics.report(fieldLocation(loc, "source"), errMissingField("file", "dataset source"))
		}
		formatLoc := fieldLocation(loc, "format")
		if declaration.Format != blueprint.DatasetFormatGeodata {
			diagnostics.errorf(fieldLocation(formatLoc, "type"), "unknown dataset format %q; want %q", declaration.Format, blueprint.DatasetFormatGeodata)
		}
		switch declaration.Kind {
		case blueprint.DatasetKindDomain, blueprint.DatasetKindIP:
			kinds[declaration.Tag] = declaration.Kind
		default:
			diagnostics.errorf(fieldLocation(formatLoc, "kind"), "unknown kind %q; want %q or %q", declaration.Kind, blueprint.DatasetKindDomain, blueprint.DatasetKindIP)
		}
		if declaration.Format == blueprint.DatasetFormatGeodata && declaration.Category == "" {
			diagnostics.report(formatLoc, errMissingField("category", "geodata format"))
		}
		if declaration.Attribute != "" && declaration.Kind == blueprint.DatasetKindIP {
			diagnostics.errorf(fieldLocation(formatLoc, "attribute"), "attribute selects domain entries; an ip dataset has none")
		}
	}
	return kinds
}

func (c routingChecker) checkMatch(fields blueprint.Match, loc string) {
	for index, tag := range fields.Inbounds {
		if !c.inbounds[tag] {
			c.diagnostics.errorf(indexLocation(fieldLocation(loc, "inbound"), index), "no inbound is tagged %q", tag)
		}
	}
	for index, family := range fields.SniffedBrowserFamilies {
		if !slices.Contains(knownBrowserFamilies, family) {
			c.diagnostics.warnf(indexLocation(fieldLocation(loc, "sniffed_browser_family"), index),
				"no sniffer reports browser family %q yet; known: %s", family, strings.Join(knownBrowserFamilies, ", "))
		}
	}
	c.checkDatasetReferences(fields.DomainDatasets, blueprint.DatasetKindDomain, fieldLocation(loc, "domain_in_dataset"))
	c.checkDatasetReferences(fields.IPDatasets, blueprint.DatasetKindIP, fieldLocation(loc, "ip_in_dataset"))
	c.checkDatasetReferences(fields.SourceIPDatasets, blueprint.DatasetKindIP, fieldLocation(loc, "source_ip_in_dataset"))

	for index, alternative := range fields.Any {
		c.checkMatch(alternative, indexLocation(fieldLocation(loc, "any"), index))
	}
	for index, required := range fields.All {
		c.checkMatch(required, indexLocation(fieldLocation(loc, "all"), index))
	}
	if fields.Not != nil {
		if isEmptyMatch(*fields.Not) {
			c.diagnostics.errorf(fieldLocation(loc, "not"), "not of an empty match matches nothing")
		}
		c.checkMatch(*fields.Not, fieldLocation(loc, "not"))
	}
}

func (c routingChecker) checkDatasetReferences(tags []string, want blueprint.DatasetKind, loc string) {
	checkDatasetReferences(c.diagnostics, c.datasetKinds, tags, want, loc)
}

func checkDatasetReferences(diagnostics *Diagnostics, datasetKinds map[string]blueprint.DatasetKind, tags []string, want blueprint.DatasetKind, loc string) {
	for index, tag := range tags {
		kind, declared := datasetKinds[tag]
		switch {
		case !declared:
			diagnostics.errorf(indexLocation(loc, index), "no dataset is tagged %q", tag)
		case kind != want:
			diagnostics.errorf(indexLocation(loc, index), "dataset %q holds %s values; this field compares %s values", tag, kind, want)
		}
	}
}

func (c routingChecker) checkAction(action blueprint.Action, loc string, fields blueprint.Match) {
	switch {
	case action.Route != nil:
		outbound, found := c.outbounds[action.Route.Outbound]
		settingsLoc := fieldLocation(loc, "settings", "outbound")
		if !found {
			c.diagnostics.errorf(settingsLoc, "no outbound is tagged %q", action.Route.Outbound)
			return
		}
		if fields.Network == spec.NetworkUDP && !carriesUDP(outbound, c.outbounds) {
			c.diagnostics.warnf(settingsLoc, "outbound %q cannot carry UDP, so UDP routed here is dropped", outbound.Tag)
		}
	case action.Resolve != nil:
		if server := action.Resolve.Server; server != "" && !c.dnsServers[server] {
			c.diagnostics.errorf(fieldLocation(loc, "settings", "server"), "no dns server is tagged %q", server)
		}
	case action.Sniff != nil:
		settingsLoc := fieldLocation(loc, "settings")
		if len(action.Sniff.Sniffers) == 0 {
			c.diagnostics.errorf(fieldLocation(settingsLoc, "sniffers"), "a sniff action needs at least one sniffer")
		}
		if action.Sniff.Timeout != nil && *action.Sniff.Timeout <= 0 {
			c.diagnostics.errorf(fieldLocation(settingsLoc, "timeout"), "timeout must be greater than zero; leave it out for the default")
		} else if action.Sniff.Timeout != nil && *action.Sniff.Timeout > longSniffTimeout {
			c.diagnostics.warnf(fieldLocation(settingsLoc, "timeout"),
				"timeout %s delays every connection whose client does not speak first, such as SSH", *action.Sniff.Timeout)
		}
		if fields.Network == spec.NetworkUDP {
			c.diagnostics.warnf(loc, "sniffing works on TCP only for now, so this sniff does nothing")
		}
		seen := make(map[string]int, len(action.Sniff.Sniffers))
		for index, sniffer := range action.Sniff.Sniffers {
			snifferLoc := indexLocation(fieldLocation(settingsLoc, "sniffers"), index)
			collectWarnings(c.diagnostics, sniffer, fieldLocation(snifferLoc, "settings"))
			name := sniffer.Info().Name
			if first, repeated := seen[name]; repeated {
				c.diagnostics.warnf(snifferLoc, "sniffer %q already appears at index %d", name, first)
				continue
			}
			seen[name] = index
		}
	}
}

func carriesUDP(outbound blueprint.Outbound, outbounds map[string]blueprint.Outbound) bool {
	return carriesUDPWithout(outbound, outbounds, map[string]bool{})
}

func carriesUDPWithout(outbound blueprint.Outbound, outbounds map[string]blueprint.Outbound, visiting map[string]bool) bool {
	if outbound.Group == nil {
		return outbound.Freedom != nil || outbound.DatagramOverStreamProxy != nil
	}
	if visiting[outbound.Tag] {
		return false
	}
	visiting[outbound.Tag] = true
	defer delete(visiting, outbound.Tag)
	for _, tag := range outbound.Group.Members {
		member, found := outbounds[tag]
		if !found || !carriesUDPWithout(member, outbounds, visiting) {
			return false
		}
	}
	return len(outbound.Group.Members) > 0
}

func endsEvaluation(action blueprint.Action) bool {
	return action.Route != nil || action.Block
}

func isEmptyMatch(fields blueprint.Match) bool {
	return len(fields.Inbounds) == 0 && fields.Network == "" && len(fields.Users) == 0 && !readsSniffResultField(fields) &&
		len(fields.Domains) == 0 && len(fields.DomainSuffixes) == 0 && len(fields.DomainKeywords) == 0 && len(fields.DomainRegexes) == 0 &&
		len(fields.IPs) == 0 && fields.IPIsPrivate == nil && len(fields.IPVersions) == 0 && len(fields.Ports) == 0 &&
		len(fields.SourceIPs) == 0 && fields.SourceIPIsPrivate == nil && len(fields.SourcePorts) == 0 &&
		len(fields.InboundLocalIPs) == 0 && len(fields.InboundLocalPorts) == 0 &&
		len(fields.DomainDatasets) == 0 && len(fields.IPDatasets) == 0 && len(fields.SourceIPDatasets) == 0 &&
		len(fields.Any) == 0 && len(fields.All) == 0 && fields.Not == nil
}

// Fields only a sniff fills; domain fields also read the destination.
func readsOnlySniffedFields(fields blueprint.Match) bool {
	return anyNestedMatch(fields, readsSniffResultField)
}

func readsSniffResultField(fields blueprint.Match) bool {
	return len(fields.SniffedProtocols) > 0 || len(fields.SniffedRequestHeaders) > 0 || len(fields.SniffedBrowserFamilies) > 0
}

func readsDestinationIP(fields blueprint.Match) bool {
	return anyNestedMatch(fields, func(each blueprint.Match) bool {
		return len(each.IPs) > 0 || each.IPIsPrivate != nil || len(each.IPVersions) > 0 || len(each.IPDatasets) > 0
	})
}

func readsSniffedData(fields blueprint.Match) bool {
	return anyNestedMatch(fields, func(each blueprint.Match) bool {
		return readsSniffResultField(each) || len(each.Domains) > 0 || len(each.DomainSuffixes) > 0 ||
			len(each.DomainKeywords) > 0 || len(each.DomainRegexes) > 0 || len(each.DomainDatasets) > 0
	})
}

func anyNestedMatch(fields blueprint.Match, test func(blueprint.Match) bool) bool {
	if test(fields) {
		return true
	}
	nested := slices.Concat(fields.Any, fields.All)
	if fields.Not != nil {
		nested = append(nested, *fields.Not)
	}
	for _, each := range nested {
		if anyNestedMatch(each, test) {
			return true
		}
	}
	return false
}

func CheckDatasets(coreBlueprint *blueprint.Blueprint) Diagnostics {
	var diagnostics Diagnostics
	if coreBlueprint == nil {
		return diagnostics
	}
	// Declarations Check already refused are not read again.
	var (
		readable  []blueprint.Dataset
		positions []int
	)
	for index, declaration := range coreBlueprint.Datasets {
		kindKnown := declaration.Kind == blueprint.DatasetKindDomain || declaration.Kind == blueprint.DatasetKindIP
		if declaration.File != "" && declaration.Format == blueprint.DatasetFormatGeodata && kindKnown {
			readable = append(readable, declaration)
			positions = append(positions, index)
		}
	}
	_, errs := dataset.LoadAll(readable)
	for index, err := range errs {
		if err != nil {
			diagnostics.report(indexLocation("datasets", positions[index]), err)
		}
	}
	return diagnostics
}
