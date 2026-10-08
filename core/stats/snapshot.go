package stats

import "time"

// Traffic is a copy of one key's Counters at one moment. Counters are read one
// at a time, so a copy taken during traffic is not one instant: bytes and
// connection counts can differ by what moved between two reads.
//
// UplinkBytes and DownlinkBytes are TCP and UDP together, so a reader that
// wants one number needs nothing else; TCP and UDP hold each network's own.
type Traffic struct {
	UplinkBytes   uint64
	DownlinkBytes uint64
	TCP           TCPTraffic
	UDP           UDPTraffic
}

// TCPTraffic is what a key carried over TCP connections.
type TCPTraffic struct {
	UplinkBytes       uint64
	DownlinkBytes     uint64
	ActiveConnections int64
	TotalConnections  uint64
}

// UDPTraffic is what a key carried as UDP datagrams. Sessions are UDP sessions
// started since the process began; there is no active count, because a UDP
// session has no end but silence, so such a count would stay high for minutes
// after the last packet. The live conversations list is what shows what is open.
type UDPTraffic struct {
	UplinkBytes     uint64
	DownlinkBytes   uint64
	UplinkPackets   uint64
	DownlinkPackets uint64
	Sessions        uint64
}

// NetworkBytes is a byte pair, one network's or the sum of both.
type NetworkBytes struct {
	UplinkBytes   uint64
	DownlinkBytes uint64
}

// TrafficTotals is what every counted inbound carried, by network and together.
type TrafficTotals struct {
	All NetworkBytes
	TCP NetworkBytes
	UDP NetworkBytes
}

// InboundTraffic adds what only an inbound sees. RefusedConnections is the sum
// of RefusalsByReason, which holds only the reasons that happened, by name.
// AuthenticationFailures is counted apart from refusals: a failed login that
// goes to the fallback is one of these and one of FallbackConnections.
type InboundTraffic struct {
	Traffic
	RefusedConnections     uint64
	RefusalsByReason       map[string]uint64
	AuthenticationFailures uint64
	FallbackConnections    uint64

	// UDP datagrams lost on the inbound's side, by reason; DatagramDrops is
	// their sum and the map holds only the reasons that happened.
	DatagramDrops         uint64
	DatagramDropsByReason map[string]uint64
}

// OutboundTraffic marks groups. A group's numbers are what its members and the
// group itself carried, so a reader summing traffic across outbounds must sum
// only the entries where IsGroup is false, or it counts the same bytes twice.
//
// DialFailures is the sum of DialFailuresByReason, which holds only the
// reasons that happened, by name.
//
// TimedDials, TotalDialTime and LastDialTime are filled only when dial health
// is on, and only for dials that connected, so the mean is TotalDialTime over
// TimedDials. A group's time is the whole dial, members it tried first
// included.
type OutboundTraffic struct {
	Traffic
	DialFailures         uint64
	DialFailuresByReason map[string]uint64

	// UDP datagrams lost on the outbound's side, by reason, as for an inbound.
	// A group counts a datagram it lost on the group, not on a member.
	DatagramDrops         uint64
	DatagramDropsByReason map[string]uint64

	TimedDials    uint64
	TotalDialTime time.Duration
	LastDialTime  time.Duration
	IsGroup       bool
}
