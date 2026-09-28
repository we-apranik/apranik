// Package component declares the roles core's own parts play for each other:
// inbounds, outbounds, the router and the traffic handler. Modules never see
// them; the module contract is spec.
package component

import (
	"context"

	"github.com/The-Apranik/Apranik/spec"
)

// Router picks the outbound for one request. It holds no state, so the same
// metadata always yields the same answer and a caller may cache it. Streams
// ask once per connection; datagrams ask once per new destination.
type Router interface {
	Route(ctx context.Context, m spec.Metadata) (Outbound, error)
}

// TrafficHandler receives what an inbound accepted: routes it and relays it.
type TrafficHandler interface {
	HandleStream(ctx context.Context, c spec.Stream, m spec.Metadata) error

	// A UDP session has no natural end, so core owns the session table
	// and the idle timeout, not the inbound.
	HandleDatagram(ctx context.Context, c spec.Datagram, m spec.Metadata) error
}

// Inbound = listener stack + proxy server.
// Not Listen: a TUN inbound reads packets off a device and accepts nothing.
type Inbound interface {
	Tag() string
	Activate(ctx context.Context, h TrafficHandler) error // returns once ready
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
	DialStream(ctx context.Context, m spec.Metadata) (spec.Stream, error)
	CreateDatagram(ctx context.Context, m spec.Metadata) (spec.Datagram, error)
	Close() error
}
