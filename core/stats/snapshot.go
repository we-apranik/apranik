package stats

import "time"

// Counters are read one at a time, so a copy taken during traffic is not one instant: bytes and
// connection counts can differ by what moved between two reads. UplinkBytes and DownlinkBytes are TCP
// and UDP together, so a reader that wants one number needs nothing else; TCP and UDP hold each
// network's own.
type Traffic struct {
	UplinkBytes   uint64
	DownlinkBytes uint64
	TCP           TCPTraffic
	UDP           UDPTraffic
}

type TCPTraffic struct {
	UplinkBytes       uint64
	DownlinkBytes     uint64
	ActiveConnections int64
	TotalConnections  uint64
}

// Sessions are UDP sessions started since the process began; there is no active count, because a UDP
// session has no end but silence, so such a count would stay high for minutes after the last packet.
type UDPTraffic struct {
	UplinkBytes     uint64
	DownlinkBytes   uint64
	UplinkPackets   uint64
	DownlinkPackets uint64
	Sessions        uint64
}

type NetworkBytes struct {
	UplinkBytes   uint64
	DownlinkBytes uint64
}

type TrafficTotals struct {
	All NetworkBytes
	TCP NetworkBytes
	UDP NetworkBytes
}

type InboundTraffic struct {
	Traffic
	RefusedConnections     uint64
	RefusalsByReason       map[string]uint64
	AuthenticationFailures uint64
	FallbackConnections    uint64

	DatagramDrops         uint64
	DatagramDropsByReason map[string]uint64
}

// A group's numbers are what its members and the group itself carried, so a reader summing traffic
// across outbounds must sum only the entries where IsGroup is false, or it counts the same bytes
// twice. TimedDials, TotalDialTime and LastDialTime are filled only when dial health is on, and only
// for dials that connected, so the mean is TotalDialTime over TimedDials.
type OutboundTraffic struct {
	Traffic
	DialFailures         uint64
	DialFailuresByReason map[string]uint64

	DatagramDrops         uint64
	DatagramDropsByReason map[string]uint64

	TimedDials    uint64
	TotalDialTime time.Duration
	LastDialTime  time.Duration
	IsGroup       bool
}
