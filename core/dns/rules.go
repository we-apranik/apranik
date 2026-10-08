package dns

import (
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/core/route/router/match"
)

// nameCondition is one compiled DNS rule condition over a lookup's name. The
// combinators mirror the router's; a lookup knows only its name, so the
// router's conditions, which read a whole request, do not fit.
type nameCondition interface {
	matches(name string) bool
}

type allNameConditions []nameCondition

func (c allNameConditions) matches(name string) bool {
	for _, condition := range c {
		if !condition.matches(name) {
			return false
		}
	}
	return true
}

type anyNameCondition []nameCondition

func (c anyNameCondition) matches(name string) bool {
	for _, condition := range c {
		if condition.matches(name) {
			return true
		}
	}
	return false
}

type notNameCondition struct{ negated nameCondition }

func (c notNameCondition) matches(name string) bool { return !c.negated.matches(name) }

type domainNameCondition []*match.DomainMatcher

func (c domainNameCondition) matches(name string) bool {
	for _, matcher := range c {
		if matcher.Match(name) {
			return true
		}
	}
	return false
}

type compiledRule struct {
	condition nameCondition
	server    *server
}

var errNonDomainField = errors.New("a dns rule matches only on the name: domain, domain_suffix, domain_keyword, domain_regex, domain_in_dataset, any, all and not")

func compileRules(rules []blueprint.DNSRule, servers map[string]*server, datasets map[string]*dataset.Dataset) ([]compiledRule, error) {
	compiled := make([]compiledRule, 0, len(rules))
	var errs []error
	for index, rule := range rules {
		condition, err := compileNameMatch(rule.Match, datasets)
		if err != nil {
			errs = append(errs, fmt.Errorf("dns.rules[%d]: %w", index, err))
			continue
		}
		target, found := servers[rule.Server]
		if !found {
			errs = append(errs, fmt.Errorf("dns.rules[%d]: no dns server %q", index, rule.Server))
			continue
		}
		compiled = append(compiled, compiledRule{condition: condition, server: target})
	}
	return compiled, errors.Join(errs...)
}

func compileNameMatch(fields blueprint.Match, datasets map[string]*dataset.Dataset) (nameCondition, error) {
	if hasNonDomainField(fields) {
		return nil, errNonDomainField
	}

	var (
		conditions allNameConditions
		errs       []error
	)
	for _, list := range []match.DomainList{
		{Exact: fields.Domains},
		{Suffixes: fields.DomainSuffixes},
		{Keywords: fields.DomainKeywords},
		{Regexes: fields.DomainRegexes},
	} {
		if matcher := match.NewDomainMatcher(list); matcher.Len() > 0 {
			conditions = append(conditions, domainNameCondition{matcher})
		}
	}
	if len(fields.DomainDatasets) > 0 {
		var matchers domainNameCondition
		for _, tag := range fields.DomainDatasets {
			loaded, found := datasets[tag]
			switch {
			case !found:
				errs = append(errs, fmt.Errorf("no dataset %q", tag))
			case loaded.Kind != blueprint.DatasetKindDomain:
				errs = append(errs, fmt.Errorf("dataset %q holds %s values, not domain", tag, loaded.Kind))
			default:
				matchers = append(matchers, loaded.Domains)
			}
		}
		conditions = append(conditions, matchers)
	}

	if len(fields.Any) > 0 {
		var alternatives anyNameCondition
		for _, alternative := range fields.Any {
			compiled, err := compileNameMatch(alternative, datasets)
			errs = append(errs, err)
			alternatives = append(alternatives, compiled)
		}
		conditions = append(conditions, alternatives)
	}
	for _, required := range fields.All {
		compiled, err := compileNameMatch(required, datasets)
		errs = append(errs, err)
		conditions = append(conditions, compiled)
	}
	if fields.Not != nil {
		compiled, err := compileNameMatch(*fields.Not, datasets)
		errs = append(errs, err)
		conditions = append(conditions, notNameCondition{compiled})
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return conditions, nil
}

func hasNonDomainField(fields blueprint.Match) bool {
	return len(fields.Inbounds) > 0 || fields.Network != "" || len(fields.Users) > 0 ||
		len(fields.SniffedProtocols) > 0 || len(fields.SniffedRequestHeaders) > 0 ||
		len(fields.SniffedBrowserFamilies) > 0 || len(fields.IPs) > 0 || fields.IPIsPrivate != nil ||
		len(fields.IPVersions) > 0 || len(fields.Ports) > 0 || len(fields.SourceIPs) > 0 ||
		fields.SourceIPIsPrivate != nil || len(fields.SourcePorts) > 0 || len(fields.InboundLocalIPs) > 0 ||
		len(fields.InboundLocalPorts) > 0 || len(fields.IPDatasets) > 0 || len(fields.SourceIPDatasets) > 0
}

// HasNonDomainField is for config.Check, which reports the field's location.
func HasNonDomainField(fields blueprint.Match) bool { return hasNonDomainField(fields) }
