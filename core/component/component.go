// Package component declares the roles core's own parts play for each other:
// inbounds, outbounds, the router and the traffic handler. Modules never see
// them; the module contract is spec.
package component

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/we-apranik/apranik/spec"
)

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

	Rule string
}

func (d Decision) IsBlock() bool { return d.Outbound == nil }

type TrafficHandler interface {
	HandleStream(ctx context.Context, clientStream spec.Stream, metadata spec.Metadata) error

	HandleDatagram(ctx context.Context, clientDatagram spec.Datagram, metadata spec.Metadata) error
}

type Inbound interface {
	Tag() string
	Activate(ctx context.Context, handler TrafficHandler) error // returns once ready
	Close() error
}

type UserManagingInbound interface {
	Inbound
	UserManager() spec.UserManager
}

type Outbound interface {
	Tag() string
	Networks() []spec.Network
	DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error)
	CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error)
	Close() error
}

// ProbingOutbound is an Outbound that tests its members in the background,
// such as an outbound group. Core calls StartProbing once, when traffic is
// about to flow, and never while building: checking a config dials nothing.
// Close stops the probing and waits for it to end.
type ProbingOutbound interface {
	Outbound
	StartProbing(ctx context.Context)
}

type GroupStateReporter interface {
	MemberTags() []string

	CurrentMemberTag() string
}

type SelectableOutbound interface {
	SelectMember(memberTag string) (previousMemberTag string, err error)
}

var (
	// A datagram that serves several destinations through one outbound opens
	// the way to a new destination on its first write (a stream to the proxy
	// server, for a protocol that names the destination in the stream request).
	// A write that fails because that did not open wraps this, so the session
	// counts it as the outbound failing to open, as it counts CreateDatagram
	// failing, not as a send that failed.
	ErrOpeningDatagramForDestination = errors.New("component: opening the way to a destination")

	ErrNoSuchMember  = errors.New("component: the group has no member with this tag")
	ErrNotSelectable = errors.New("component: this outbound is not a group a person drives")
)

type CarryingOutboundsReporter interface {
	CarryingOutboundTags() []string
}

func TagsOfOutboundsThatCarried(outbound Outbound, carrier any) []string {
	reporter, reports := carrier.(CarryingOutboundsReporter)
	if !reports {
		return []string{outbound.Tag()}
	}
	beneath := reporter.CarryingOutboundTags()
	tags := make([]string, 0, 1+len(beneath))
	tags = append(tags, outbound.Tag())
	return append(tags, beneath...)
}
