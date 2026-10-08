package blueprint

import (
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// DNS is the resolver's configuration: which DNS servers answer
// which names, how answers are cached, and which IP versions a lookup asks for.
// Nil means one system server with every default, as before DNS existed.
type DNS struct {
	// Zero means DefaultIPVersions.
	IPVersions IPVersions

	Cache DNSCache

	Servers []DNSServer

	// Evaluated in order; the first match picks the server, and a name no rule
	// matches goes to DefaultServer.
	Rules         []DNSRule
	DefaultServer string
}

// DefaultIPVersions is used when neither the dns section nor a server says.
// PROVISIONAL: IPv4 first, because IPv6 is missing or filtered on many of the
// networks Apranik runs on; the other family is still tried.
const DefaultIPVersions = IPVersionsPreferIPv4

// IPVersions says which address records a lookup asks for, and in which order
// the addresses come back.
type IPVersions string

const (
	IPVersionsIPv4Only   IPVersions = "ipv4_only"
	IPVersionsIPv6Only   IPVersions = "ipv6_only"
	IPVersionsPreferIPv4 IPVersions = "prefer_ipv4"
	IPVersionsPreferIPv6 IPVersions = "prefer_ipv6"
)

func (v IPVersions) Valid() bool {
	switch v {
	case IPVersionsIPv4Only, IPVersionsIPv6Only, IPVersionsPreferIPv4, IPVersionsPreferIPv6:
		return true
	}
	return false
}

// DNSCache: a nil field means the default the resolver documents; a value
// must be positive.
type DNSCache struct {
	Disable bool

	// Nil means the resolver's default.
	MaxEntries *int

	// A separate cache, with its own MaxEntries, for each server.
	PerServer bool

	// Serve an expired answer at once and refresh it in the background, while
	// it is no older than MaxStaleAge past its expiry (RFC 8767).
	ServeStale bool

	// Nil means the resolver's default.
	MaxStaleAge *time.Duration
}

// DNSServer is one upstream the resolver sends queries to. Exactly one of
// Stream, Datagram and System is set.
type DNSServer struct {
	Tag string

	Stream   spec.StreamDNSServerModule
	Datagram spec.DatagramDNSServerModule

	// The special type system, built by core: the operating system's resolver.
	System *SystemDNSServer

	// The TLS layer under a Stream module that NeedsTLS: the user's tls block
	// decoded into the registry's tls module, or that module with its defaults.
	TLS spec.OverlayStreamTransportModule

	// The outbound the server's traffic takes. Empty means the internal
	// freedom DefaultDNSOutboundTag. Not used by System.
	Outbound string

	// Zero means the dns section's.
	IPVersions IPVersions

	// The server that resolves this server's own hostname. Empty means
	// DefaultServer.
	DNSServer string
}

// DefaultDNSOutboundTag is the internal freedom core builds for DNS servers
// that name no outbound. A user outbound may not take it.
const DefaultDNSOutboundTag = "dns-default-freedom"

// SystemDNSServer asks the operating system, which takes a name and returns
// addresses: no wire format, no outbound, and no forwarded client queries.
type SystemDNSServer struct {
	// Nil means the resolver's default; a value must be positive.
	Timeout *time.Duration
}

// DNSRule's Match uses only the domain fields, Any, All and Not: a lookup has
// a name and nothing else. config.Check refuses the others.
type DNSRule struct {
	Match  Match
	Server string
}
