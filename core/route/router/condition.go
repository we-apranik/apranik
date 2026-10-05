package router

import (
	"net/netip"
	"regexp"
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
	return request.anyDestinationIP(func(address netip.Addr) bool { return containedInAny(c, address) })
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
	return request.anyDestinationIP(func(address netip.Addr) bool { return match.IsPrivateAddress(address) == bool(c) })
}

type destinationPortCondition struct{ ports *match.PortMatcher }

func (c destinationPortCondition) matches(request *routedRequest) bool {
	return c.ports.Contains(request.metadata.Destination.Port())
}

type sourcePortCondition struct{ ports *match.PortMatcher }

func (c sourcePortCondition) matches(request *routedRequest) bool {
	return request.metadata.Transport.SourceEndpoint.IsValid() && c.ports.Contains(request.metadata.Transport.SourceEndpoint.Port())
}

// Every named header must be present and match; header names are lowercase.
type sniffedRequestHeaderCondition map[string]*regexp.Regexp

func (c sniffedRequestHeaderCondition) matches(request *routedRequest) bool {
	sniffed := request.metadata.Sniffed
	if sniffed == nil {
		return false
	}
	for name, pattern := range c {
		value, present := sniffed.RequestHeaders[name]
		if !present || !pattern.MatchString(value) {
			return false
		}
	}
	return true
}

type sniffedBrowserFamilyCondition []string

func (c sniffedBrowserFamilyCondition) matches(request *routedRequest) bool {
	sniffed := request.metadata.Sniffed
	return sniffed != nil && sniffed.BrowserFamily != "" && slices.Contains(c, sniffed.BrowserFamily)
}

// A domain destination matches no version until a resolve action has run.
type ipVersionCondition []int

func (c ipVersionCondition) matches(request *routedRequest) bool {
	return request.anyDestinationIP(func(address netip.Addr) bool {
		version := 6
		if address.Unmap().Is4() {
			version = 4
		}
		return slices.Contains(c, version)
	})
}

type sourceIPIsPrivateCondition bool

func (c sourceIPIsPrivateCondition) matches(request *routedRequest) bool {
	address, valid := request.sourceIP()
	return valid && match.IsPrivateAddress(address) == bool(c)
}

type inboundLocalIPCondition []*match.PrefixMatcher

func (c inboundLocalIPCondition) matches(request *routedRequest) bool {
	address := request.metadata.Transport.InboundLocalEndpoint.Addr()
	return address.IsValid() && containedInAny(c, address)
}

type inboundLocalPortCondition struct{ ports *match.PortMatcher }

func (c inboundLocalPortCondition) matches(request *routedRequest) bool {
	localEndpoint := request.metadata.Transport.InboundLocalEndpoint
	return localEndpoint.IsValid() && c.ports.Contains(localEndpoint.Port())
}
