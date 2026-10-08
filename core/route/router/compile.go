package router

import (
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/core/route/router/match"
	"github.com/The-Apranik/Apranik/spec"
)

// config.Check reports every problem found here with its location first; these
// errors are for a blueprint written in Go that skipped it.
type ruleCompiler struct {
	outbounds map[string]component.Outbound
	datasets  map[string]*dataset.Dataset
}

// Each field becomes one condition; a match holds only if all of them do.
func (c ruleCompiler) compileMatch(fields blueprint.Match) (condition, error) {
	var (
		conditions allOf
		errs       []error
	)
	add := func(each condition) { conditions = append(conditions, each) }

	if len(fields.Inbounds) > 0 {
		add(inboundCondition(fields.Inbounds))
	}
	if fields.Network != "" {
		add(networkCondition(fields.Network))
	}
	if len(fields.Users) > 0 {
		add(userCondition(fields.Users))
	}
	if len(fields.SniffedProtocols) > 0 {
		add(sniffedProtocolCondition(fields.SniffedProtocols))
	}
	if len(fields.SniffedRequestHeaders) > 0 {
		add(sniffedRequestHeaderCondition(fields.SniffedRequestHeaders))
	}
	if len(fields.SniffedBrowserFamilies) > 0 {
		add(sniffedBrowserFamilyCondition(fields.SniffedBrowserFamilies))
	}

	for _, list := range []match.DomainList{
		{Exact: fields.Domains},
		{Suffixes: fields.DomainSuffixes},
		{Keywords: fields.DomainKeywords},
		{Regexes: fields.DomainRegexes},
	} {
		if matcher := match.NewDomainMatcher(list); matcher.Len() > 0 {
			add(domainCondition{matcher})
		}
	}
	if len(fields.DomainDatasets) > 0 {
		matchers, err := c.domainDatasets(fields.DomainDatasets)
		errs = append(errs, err)
		add(domainCondition(matchers))
	}

	if len(fields.IPs) > 0 {
		add(destinationIPCondition{match.NewPrefixMatcher(fields.IPs)})
	}
	if len(fields.IPDatasets) > 0 {
		matchers, err := c.ipDatasets(fields.IPDatasets)
		errs = append(errs, err)
		add(destinationIPCondition(matchers))
	}
	if fields.IPIsPrivate != nil {
		add(ipIsPrivateCondition(*fields.IPIsPrivate))
	}
	if len(fields.IPVersions) > 0 {
		add(ipVersionCondition(fields.IPVersions))
	}
	if len(fields.Ports) > 0 {
		add(destinationPortCondition{match.NewPortMatcher(portRanges(fields.Ports))})
	}

	if len(fields.SourceIPs) > 0 {
		add(sourceIPCondition{match.NewPrefixMatcher(fields.SourceIPs)})
	}
	if len(fields.SourceIPDatasets) > 0 {
		matchers, err := c.ipDatasets(fields.SourceIPDatasets)
		errs = append(errs, err)
		add(sourceIPCondition(matchers))
	}
	if len(fields.SourcePorts) > 0 {
		add(sourcePortCondition{match.NewPortMatcher(portRanges(fields.SourcePorts))})
	}
	if fields.SourceIPIsPrivate != nil {
		add(sourceIPIsPrivateCondition(*fields.SourceIPIsPrivate))
	}

	if len(fields.InboundLocalIPs) > 0 {
		add(inboundLocalIPCondition{match.NewPrefixMatcher(fields.InboundLocalIPs)})
	}
	if len(fields.InboundLocalPorts) > 0 {
		add(inboundLocalPortCondition{match.NewPortMatcher(portRanges(fields.InboundLocalPorts))})
	}

	if len(fields.Any) > 0 {
		var alternatives anyOf
		for _, alternative := range fields.Any {
			compiled, err := c.compileMatch(alternative)
			errs = append(errs, err)
			alternatives = append(alternatives, compiled)
		}
		add(alternatives)
	}
	for _, required := range fields.All {
		compiled, err := c.compileMatch(required)
		errs = append(errs, err)
		add(compiled)
	}
	if fields.Not != nil {
		compiled, err := c.compileMatch(*fields.Not)
		errs = append(errs, err)
		add(notCondition{compiled})
	}

	return conditions, errors.Join(errs...)
}

func (c ruleCompiler) domainDatasets(tags []string) ([]*match.DomainMatcher, error) {
	var matchers []*match.DomainMatcher
	for _, tag := range tags {
		loaded, err := c.dataset(tag, blueprint.DatasetKindDomain)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, loaded.Domains)
	}
	return matchers, nil
}

func (c ruleCompiler) ipDatasets(tags []string) ([]*match.PrefixMatcher, error) {
	var matchers []*match.PrefixMatcher
	for _, tag := range tags {
		loaded, err := c.dataset(tag, blueprint.DatasetKindIP)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, loaded.IPs)
	}
	return matchers, nil
}

func (c ruleCompiler) dataset(tag string, kind blueprint.DatasetKind) (*dataset.Dataset, error) {
	loaded, found := c.datasets[tag]
	if !found {
		return nil, fmt.Errorf("no dataset %q", tag)
	}
	if loaded.Kind != kind {
		return nil, fmt.Errorf("dataset %q holds %s values, not %s", tag, loaded.Kind, kind)
	}
	return loaded, nil
}

func (c ruleCompiler) compileAction(action blueprint.Action) (compiledAction, error) {
	set := 0
	for _, isSet := range []bool{action.Route != nil, action.Block, action.Sniff != nil, action.Resolve != nil} {
		if isSet {
			set++
		}
	}
	if set != 1 {
		return compiledAction{}, errors.New("an action must be exactly one of route, block, sniff and resolve")
	}

	switch {
	case action.Resolve != nil:
		return compiledAction{kind: actionResolve, resolveServer: action.Resolve.Server}, nil
	case action.Route != nil:
		outbound, found := c.outbounds[action.Route.Outbound]
		if !found {
			return compiledAction{}, fmt.Errorf("no outbound %q", action.Route.Outbound)
		}
		return compiledAction{kind: actionRoute, outbound: outbound}, nil
	case action.Block:
		return compiledAction{kind: actionBlock}, nil
	default:
		sniffers := make([]spec.Sniffer, 0, len(action.Sniff.Sniffers))
		for _, snifferModule := range action.Sniff.Sniffers {
			sniffer, err := snifferModule.NewSniffer()
			if err != nil {
				return compiledAction{}, fmt.Errorf("sniffer %q: %w", snifferModule.Info().Name, err)
			}
			sniffers = append(sniffers, sniffer)
		}
		return compiledAction{kind: actionSniff, sniffers: sniffers, sniffTimeout: types.ValueOrDefault(action.Sniff.Timeout, 0)}, nil
	}
}

func portRanges(ranges []blueprint.PortRange) []match.PortRange {
	converted := make([]match.PortRange, len(ranges))
	for index, portRange := range ranges {
		converted[index] = match.PortRange{First: portRange.First, Last: portRange.Last}
	}
	return converted
}
