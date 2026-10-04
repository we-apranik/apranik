// Package blueprint describes what to build: every inbound and outbound, with
// each module looked up, decoded and validated, but nothing opened. Keeping
// that apart from building lets a config file be checked on a machine that could
// never run it, and lets an embedder write one in Go without JSON.
package blueprint

import (
	"time"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/spec"
)

// Version is what a config file's min_core is compared against.
const Version = "0.1.0"

// SchemaVersion is the config file's "version" field.
const SchemaVersion = 1

type Blueprint struct {
	Version   int
	Log       Log
	Inbounds  []Inbound
	Outbounds []Outbound

	// Named lists of values that routing and DNS rules look up.
	Datasets []Dataset

	// Nil means every name is resolved by the operating system, as before
	// DNS existed.
	DNS *DNS

	// Nil means every request takes the first outbound.
	Routing *Routing
}

// Log's zero value is a silent logger.
type Log = logging.Config

type StreamTransportStack struct {
	Base     spec.BaseStreamTransportModule
	Overlays []spec.OverlayStreamTransportModule
}

type DatagramTransportStack struct {
	Base     spec.BaseDatagramTransportModule
	Overlays []spec.OverlayDatagramTransportModule
}

func (s StreamTransportStack) IsZero() bool   { return s.Base == nil }
func (s DatagramTransportStack) IsZero() bool { return s.Base == nil }

// Network says which of the two stacks and which protocol field (StreamProxy,
// Mixed or DatagramProxy) are in use; config.Check verifies they agree.
type Inbound struct {
	Tag string

	// Required, with no default: an inbound that silently bound every
	// interface because a field was omitted is a real exposure.
	Bind spec.Endpoint

	Network spec.Network

	StreamTransport   StreamTransportStack
	DatagramTransport DatagramTransportStack

	StreamProxy   spec.StreamProxyModule
	DatagramProxy spec.DatagramProxyModule

	// The special protocol mixed, in place of StreamProxy.
	Mixed *Mixed

	// Set when the stream proxy's module also registered the
	// datagram-over-stream kind: one handshake may then ask for either.
	DatagramOverStreamProxy spec.DatagramOverStreamProxyModule

	// Where a stream goes when the proxy answers spec.ErrFallbackRequested.
	// Zero means close it instead.
	Fallback spec.Destination

	// Silence in both directions ends a TCP connection. Zero means
	// relay.DefaultStreamIdleTimeout; negative means no bound.
	TCPIdleTimeout time.Duration

	// Silence ends a UDP session. Zero means session.DefaultIdleTimeout.
	UDPSessionIdleTimeout time.Duration

	// Connections held at once; one more is closed as soon as it is accepted.
	// Zero means no limit.
	MaxConnections int
}

// Exactly one of Proxy and Freedom is set; config.Check refuses anything
// else. Blocking is a routing action, not an outbound.
type Outbound struct {
	Tag string

	// The proxy server's address, not the user's destination. Only with Proxy.
	Server spec.Destination

	// Only with Proxy: freedom reaches no server.
	StreamTransport StreamTransportStack

	Proxy spec.StreamProxyModule

	// Only with Proxy, and only when its module also registered the
	// datagram-over-stream kind: then this outbound carries UDP too.
	DatagramOverStreamProxy spec.DatagramOverStreamProxyModule

	Freedom *Freedom

	// Only with Proxy: the DNS server that resolves Server when it is a
	// domain, so the proxy's own name never loops through a DNS server that
	// uses the proxy. Empty means the dns section's default server.
	DNSServer string
}

// Mixed serves SOCKS5 and HTTP on one port; core picks one per connection
// from its first byte.
type Mixed struct {
	SOCKS5 spec.StreamProxyModule
	HTTP   spec.StreamProxyModule

	// How long a client may take to send the byte that picks SOCKS5 or HTTP;
	// each then bounds its own handshake. Zero means
	// mixed.DefaultFirstByteTimeout.
	FirstByteTimeout time.Duration
}

// Freedom sends straight to each destination, so its transports are always
// plain TCP and UDP; only their settings are configurable.
type Freedom struct {
	StreamTransport   StreamTransportStack
	DatagramTransport DatagramTransportStack

	// Nil means nat.NewDefaultPolicy().
	NAT *nat.Policy
}
