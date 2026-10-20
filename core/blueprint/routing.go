package blueprint

import (
	"net/netip"
	"regexp"
	"time"

	"github.com/we-apranik/apranik/spec"
)

type Routing struct {
	Rules         []Rule
	DefaultAction Action
}

type Rule struct {
	Match  Match
	Action Action
}

// Match holds a rule's conditions. Different fields must all hold; the values
// of one field are alternatives. An empty Match matches every request.
//
// Domain fields read the sniffed name when one was sniffed, else the
// destination's domain. IP fields read the destination's IP, or, after a
// resolve action, match when any resolved address does; the router never
// resolves on its own.
type Match struct {
	Inbounds         []string
	Network          spec.Network // zero: either
	Users            []string
	SniffedProtocols []string

	SniffedRequestHeaders  map[string]*regexp.Regexp
	SniffedBrowserFamilies []string

	Domains        []string
	DomainSuffixes []string
	DomainKeywords []string
	DomainRegexes  []*regexp.Regexp

	IPs         []netip.Prefix
	IPIsPrivate *bool
	IPVersions  []int // 4 or 6; a domain destination matches neither
	Ports       []PortRange

	SourceIPs         []netip.Prefix
	SourceIPIsPrivate *bool
	SourcePorts       []PortRange

	InboundLocalIPs   []netip.Prefix
	InboundLocalPorts []PortRange

	DomainDatasets   []string
	IPDatasets       []string
	SourceIPDatasets []string

	Any []Match
	All []Match
	Not *Match
}

type PortRange struct {
	First, Last uint16
}

// Exactly one of Route, Block, Sniff and Resolve is set. Route and Block end
// the rule list; Sniff fills the sniff result and Resolve the destination's
// addresses, and both go on to the next rule.
type Action struct {
	Route   *RouteAction
	Block   bool
	Sniff   *SniffAction
	Resolve *ResolveAction
}

// ResolveAction looks the destination's domain up so IP fields can match it.
// The destination itself is never rewritten.
type ResolveAction struct {
	Server string
}

type RouteAction struct {
	Outbound string
}

type SniffAction struct {
	Sniffers []spec.SnifferModule

	Timeout *time.Duration
}

type Dataset struct {
	Tag  string
	Kind DatasetKind

	File string

	Format string

	Category  string
	Attribute string
}

type DatasetKind string

const (
	DatasetKindDomain DatasetKind = "domain"
	DatasetKindIP     DatasetKind = "ip"
)

const DatasetFormatGeodata = "geodata"
