package blueprint

import (
	"time"

	"github.com/we-apranik/apranik/spec"
)

type DNS struct {
	IPVersions IPVersions

	Cache DNSCache

	Servers []DNSServer

	Rules         []DNSRule
	DefaultServer string
}

// DefaultIPVersions is used when neither the dns section nor a server says.
// PROVISIONAL: IPv4 first, because IPv6 is missing or filtered on many of the
// networks Apranik runs on; the other family is still tried.
const DefaultIPVersions = IPVersionsPreferIPv4

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

type DNSCache struct {
	Disable bool

	MaxEntries *int

	PerServer bool

	// Serve an expired answer at once and refresh it in the background, while
	// it is no older than MaxStaleAge past its expiry (RFC 8767).
	ServeStale bool

	MaxStaleAge *time.Duration
}

type DNSServer struct {
	Tag string

	Stream   spec.StreamDNSServerModule
	Datagram spec.DatagramDNSServerModule

	System *SystemDNSServer

	// The TLS layer under a Stream module that NeedsTLS: the user's tls block
	// decoded into the registry's tls module, or that module with its defaults.
	TLS spec.OverlayStreamTransportModule

	Outbound string

	IPVersions IPVersions

	DNSServer string
}

const DefaultDNSOutboundTag = "dns-default-freedom"

type SystemDNSServer struct {
	Timeout *time.Duration
}

type DNSRule struct {
	Match  Match
	Server string
}
