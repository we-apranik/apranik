// Package blueprint describes what to build: every inbound and outbound, with
// each module looked up, decoded and validated, but nothing opened. Keeping
// that apart from building lets a config file be checked on a machine that could
// never run it, and lets an embedder write one in Go without JSON.
package blueprint

import (
	"encoding/json"
	"time"

	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/udp/nat"
	"github.com/we-apranik/apranik/spec"
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

	// Nil counts everything.
	Stats *Stats

	// The ways other programs read and drive a running Apranik. Empty means
	// none; each is started after everything else is running.
	APIs []spec.APIModule
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

	// Only with StreamProxy: what the server accepts of Mux.Cool. Nil means the
	// defaults.
	MuxCool *MuxCool

	// Silence in both directions ends a TCP connection. Nil means
	// relay.DefaultStreamIdleTimeout; negative means no bound; zero is refused.
	TCPIdleTimeout *time.Duration

	// Silence ends a UDP session. Nil means session.DefaultIdleTimeout; a
	// value must be positive.
	UDPSessionIdleTimeout *time.Duration

	// The largest UDP datagram a client may send and a reply may be; a bigger
	// one is dropped and counted. Nil means udp.DefaultMaxDatagramSize; a
	// value must be positive.
	UDPMaxDatagramSize *int

	// Silence from the client to one destination of a UDP session makes the
	// session forget that destination's routing decision. Nil means
	// udp.DefaultRoutingCacheEntryIdleTimeout; a value must be positive.
	UDPRoutingCacheEntryIdleTimeout *time.Duration

	// How many destinations a UDP session remembers at once; the one used
	// longest ago is forgotten when it is full. Nil means
	// udp.DefaultRoutingCacheMaxEntriesPerSession; a value must be positive.
	UDPRoutingCacheMaxEntriesPerSession *int

	// TCP connections held at once; one more is closed as soon as it is
	// accepted. Nil means no limit; a value must be positive.
	MaxTCPConnections *int

	// UDP sessions held at once, over a stream protocol that carries UDP (a
	// SOCKS5 association, Trojan's UDP); a request for one more is refused.
	// Nil means no limit; a value must be positive.
	MaxUDPSessions *int
}

// Exactly one of Proxy, Freedom and Group is set; config.Check refuses anything
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

	// A group reaches no server and takes no transport: it picks among other
	// outbounds.
	Group *Group

	// Only with Proxy: how the outbound uses Mux.Cool. Nil means the defaults.
	MuxCool *MuxCool

	// Only with Proxy: when its client sends the request it owes the server. Nil
	// means the defaults.
	ProxyRequestTiming *ProxyRequestTiming

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

	// The inbound's users, one list for both protocols, each a JSON object as
	// SOCKS5 and HTTP would write a user. Core hands them to both servers
	// without reading them, so a user is valid only if both protocols accept
	// it. Empty means no authentication.
	Users []json.RawMessage

	// How long a client may take to send the byte that picks SOCKS5 or HTTP;
	// each then bounds its own handshake. Nil means
	// mixed.DefaultFirstByteTimeout; a value must be positive.
	FirstByteTimeout *time.Duration
}

// Freedom sends straight to each destination, so its transports are always
// plain TCP and UDP; only their settings are configurable.
type Freedom struct {
	StreamTransport   StreamTransportStack
	DatagramTransport DatagramTransportStack

	// Nil means nat.NewDefaultPolicy().
	NAT *nat.Policy

	// The largest reply the NAT passes on; a bigger one is dropped and
	// counted. Nil means udp.DefaultMaxDatagramSize; a value must be positive.
	NATMaxDatagramSize *int
}
