// Package component declares the roles core's own parts play for each other:
// inbounds, outbounds, the router and the traffic handler. Modules never see
// them; the module contract is spec.
package component

import (
	"context"
	"net/netip"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// Router decides where one request goes. It is shared by every inbound and
// holds no per-request state; what it needs to know about the request, and
// what it can find out on demand, it asks the informant. Streams ask once per
// connection; datagrams ask once per new destination in a UDP session.
type Router interface {
	Route(ctx context.Context, informant RouterTrafficInformant) (Decision, error)
}

// RouterTrafficInformant tells the router about the traffic it is deciding
// on: what is known from the handshake, and what costs work to find out,
// which it finds out only when a rule asks and then remembers. The handler
// makes one per connection, or per destination of a UDP session, and keeps
// the stream to itself: the router never touches traffic.
type RouterTrafficInformant interface {
	Metadata() spec.Metadata

	// Sniff peeks at the start of the client's payload. Only the first call
	// sniffs; later calls return its result. Nil when nothing was recognised
	// or the traffic cannot be sniffed, such as a UDP session.
	Sniff(ctx context.Context, sniffers []spec.Sniffer, timeout time.Duration) *spec.SniffResult

	// Resolve looks the destination's domain up, asking the DNS server tagged
	// serverTag, or the one the DNS rules pick when it is empty.
	// Only the first call resolves; later calls return its result. Nil when
	// the destination is an IP, or the lookup failed. The destination itself
	// is never rewritten.
	Resolve(ctx context.Context, serverTag string) []netip.Addr
}

// Decision is the router's answer. A nil Outbound means the request is
// blocked.
type Decision struct {
	Outbound Outbound

	// Which rule decided, for logs: "routing.rules[3]", or "default".
	Rule string
}

func (d Decision) IsBlock() bool { return d.Outbound == nil }

// TrafficHandler receives what an inbound accepted: routes it and relays it.
type TrafficHandler interface {
	HandleStream(ctx context.Context, clientStream spec.Stream, metadata spec.Metadata) error

	// A UDP session has no natural end, so core owns the session table
	// and the idle timeout, not the inbound.
	HandleDatagram(ctx context.Context, clientDatagram spec.Datagram, metadata spec.Metadata) error
}

// Inbound = listener stack + proxy server.
// Not Listen: a TUN inbound reads packets off a device and accepts nothing.
type Inbound interface {
	Tag() string
	Activate(ctx context.Context, handler TrafficHandler) error // returns once ready
	Close() error
}

// Outbound = proxy client + dialer stack.
//
// Networks reports what this outbound can actually carry, so routing can rule
// it out before a packet arrives rather than discovering it on a failed dial.
// Calling a method for a network Networks does not list returns
// spec.ErrUnsupportedNetwork, but that is an invariant violation, not a path
// routing is expected to take.
//
// Shares a name with spec.StreamDialer.DialStream at a different signature, so
// no type can be both. Accepted: core only ever holds an Outbound as an
// Outbound. An Outbound that must act as an underlying dialer needs an adapter
// holding it as a field rather than embedding it.
type Outbound interface {
	Tag() string
	Networks() []spec.Network
	DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error)
	CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error)
	Close() error
}
