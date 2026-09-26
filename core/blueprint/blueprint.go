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
}

// Log's zero value is a silent logger.
type Log = logging.Config

type StreamTransportStack struct {
	Base     spec.BaseStreamTransport
	Overlays []spec.OverlayStreamTransport
}

type DatagramTransportStack struct {
	Base     spec.BaseDatagramTransport
	Overlays []spec.OverlayDatagramTransport
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

	StreamProxy   spec.StreamProxy
	DatagramProxy spec.DatagramProxy

	// The special protocol mixed, in place of StreamProxy.
	Mixed *Mixed

	// Set when the stream proxy's module also registered the
	// datagram-over-stream kind: one handshake may then ask for either.
	DatagramOverStreamProxy spec.DatagramOverStreamProxy

	// Where a stream goes when the proxy answers spec.ErrFallbackRequested.
	// Zero means close it instead.
	Fallback spec.Destination

	// Bounds the proxy handshake, so a client that connects and says nothing
	// cannot hold a goroutine open indefinitely. Zero leaves the inbound's own
	// default in force.
	HandshakeTimeout time.Duration

	// Silence in both directions ends a TCP connection. Zero means
	// relay.DefaultStreamIdleTimeout; negative means no bound.
	TCPIdleTimeout time.Duration

	// Silence ends a UDP session. Zero means session.DefaultIdleTimeout.
	UDPSessionIdleTimeout time.Duration

	// Connections held at once; one more is closed as soon as it is accepted.
	// Zero means no limit.
	MaxConnections int

	// Nil means no sniffing, the default.
	Sniffing *Sniffing
}

// Sniffing runs after the handshake and before routing, and only fills
// Metadata.Sniffed: the destination is never rewritten.
type Sniffing struct {
	Sniffers []spec.Sniffer

	// Zero means sniff.DefaultTimeout.
	Timeout time.Duration
}

// Exactly one of Proxy, Freedom and Block is set; config.Check refuses
// anything else.
type Outbound struct {
	Tag string

	// The proxy server's address, not the user's destination. Only with Proxy.
	Server spec.Destination

	// Only with Proxy: freedom and block reach no server.
	StreamTransport StreamTransportStack

	Proxy spec.StreamProxy

	// Only with Proxy, and only when its module also registered the
	// datagram-over-stream kind: then this outbound carries UDP too.
	DatagramOverStreamProxy spec.DatagramOverStreamProxy

	Freedom *Freedom
	Block   bool

	// Zero means outbound.DefaultDialTimeout; negative means no bound. Not
	// with Block, which dials nothing.
	DialTimeout time.Duration

	// The proxy handshake with the server; only with Proxy. Zero means
	// outbound.DefaultHandshakeTimeout; negative means no bound.
	HandshakeTimeout time.Duration
}

// Mixed serves SOCKS5 and HTTP on one port; core picks one per connection
// from its first byte.
type Mixed struct {
	SOCKS5 spec.StreamProxy
	HTTP   spec.StreamProxy
}

// Freedom sends straight to each destination, so its transports are always
// plain TCP and UDP; only their settings are configurable.
type Freedom struct {
	StreamTransport   StreamTransportStack
	DatagramTransport DatagramTransportStack

	// Nil means nat.NewDefaultPolicy().
	NAT *nat.Policy
}
