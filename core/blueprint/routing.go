package blueprint

import (
	"net/netip"
	"regexp"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// Routing decides which outbound serves each request: the first rule whose
// action decides wins, and DefaultAction serves the rest.
type Routing struct {
	Datasets      []Dataset
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
// destination's domain. IP fields read the destination's IP only: a domain
// destination is never resolved to match them.
type Match struct {
	Inbounds         []string
	Network          spec.Network // zero: either
	Users            []string
	SniffedProtocols []string

	// Lowercase, without a trailing dot; suffixes also without a leading one.
	Domains        []string
	DomainSuffixes []string
	DomainKeywords []string
	DomainRegexes  []*regexp.Regexp

	// A single address is a /32 or /128.
	IPs         []netip.Prefix
	IPIsPrivate *bool
	Ports       []PortRange

	SourceIPs   []netip.Prefix
	SourcePorts []PortRange

	// Dataset tags, each of the kind the field compares.
	DomainDatasets   []string
	IPDatasets       []string
	SourceIPDatasets []string

	Any []Match
	All []Match
	Not *Match
}

// PortRange is inclusive; a single port has First == Last.
type PortRange struct {
	First, Last uint16
}

// Exactly one of Route, Block and Sniff is set. Route and Block end the rule
// list; Sniff fills the sniff result and goes on to the next rule.
type Action struct {
	Route *RouteAction
	Block bool
	Sniff *SniffAction
}

type RouteAction struct {
	Outbound string
}

type SniffAction struct {
	Sniffers []spec.SnifferModule

	// Zero means sniff.DefaultTimeout.
	Timeout time.Duration
}

// Dataset is a named list of values rules look up: domains or IP
// ranges, read from a file when the router is built. Nothing is read here.
type Dataset struct {
	Tag  string
	Kind DatasetKind

	// Absolute when the config was loaded through config.LoadFile or LoadURL.
	File string

	// The file's format: "geodata" for V2Ray's geosite.dat and geoip.dat.
	Format string

	// geodata: the list inside the file, and optionally only the entries
	// carrying an attribute.
	Category  string
	Attribute string
}

type DatasetKind string

const (
	DatasetKindDomain DatasetKind = "domain"
	DatasetKindIP     DatasetKind = "ip"
)

const DatasetFormatGeodata = "geodata"
