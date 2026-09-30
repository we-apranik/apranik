package router

import (
	"net/netip"
	"slices"

	"github.com/The-Apranik/Apranik/core/route/match"
	"github.com/The-Apranik/Apranik/spec"
)

// condition is one compiled field of a match, or a whole match.
type condition interface {
	matches(request *routedRequest) bool
}

// Every condition must hold; none at all matches every request.
type allOf []condition

func (c allOf) matches(request *routedRequest) bool {
	for _, each := range c {
		if !each.matches(request) {
			return false
		}
	}
	return true
}

type anyOf []condition

func (c anyOf) matches(request *routedRequest) bool {
	for _, each := range c {
		if each.matches(request) {
			return true
		}
	}
	return false
}

type notCondition struct{ negated condition }

func (c notCondition) matches(request *routedRequest) bool { return !c.negated.matches(request) }

type inboundCondition []string

func (c inboundCondition) matches(request *routedRequest) bool {
	return slices.Contains(c, request.metadata.InboundTag)
}

type networkCondition spec.Network

func (c networkCondition) matches(request *routedRequest) bool {
	return request.metadata.Network == spec.Network(c)
}

type userCondition []string

func (c userCondition) matches(request *routedRequest) bool {
	return request.metadata.User != "" && slices.Contains(c, request.metadata.User)
}

type sniffedProtocolCondition []string

func (c sniffedProtocolCondition) matches(request *routedRequest) bool {
	sniffed := request.metadata.Sniffed
	return sniffed != nil && slices.Contains(c, sniffed.Protocol)
}

// Literal domains and domain datasets alike: any matcher matching the name.
type domainCondition []*match.DomainMatcher

func (c domainCondition) matches(request *routedRequest) bool {
	name := request.domainName()
	if name == "" {
		return false
	}
	for _, matcher := range c {
		if matcher.Match(name) {
			return true
		}
	}
	return false
}

type destinationIPCondition []*match.PrefixMatcher

func (c destinationIPCondition) matches(request *routedRequest) bool {
	address, isIP := request.destinationIP()
	return isIP && containedInAny(c, address)
}

type sourceIPCondition []*match.PrefixMatcher

func (c sourceIPCondition) matches(request *routedRequest) bool {
	address, valid := request.sourceIP()
	return valid && containedInAny(c, address)
}

func containedInAny(matchers []*match.PrefixMatcher, address netip.Addr) bool {
	for _, matcher := range matchers {
		if matcher.Contains(address) {
			return true
		}
	}
	return false
}

type ipIsPrivateCondition bool

func (c ipIsPrivateCondition) matches(request *routedRequest) bool {
	address, isIP := request.destinationIP()
	return isIP && match.IsPrivateAddress(address) == bool(c)
}

type destinationPortCondition struct{ ports *match.PortMatcher }

func (c destinationPortCondition) matches(request *routedRequest) bool {
	return c.ports.Contains(request.metadata.Destination.Port())
}

type sourcePortCondition struct{ ports *match.PortMatcher }

func (c sourcePortCondition) matches(request *routedRequest) bool {
	return request.metadata.Source.IsValid() && c.ports.Contains(request.metadata.Source.Port())
}
