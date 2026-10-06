// Package stats counts traffic for the operator: payload bytes and connections,
// per inbound, per outbound and per user. Core counts; no module is involved.
// Counters only go up (active connections aside) for the life of the process,
// so any number of readers can take snapshots and subtract. Nothing is
// persisted.
package stats

import (
	"sync/atomic"
	"time"
)

// Counters is what every key has: an inbound tag, an outbound tag or a user.
// Uplink is client to remote and downlink is remote to client, from the
// user's side. Bytes are payload, above the proxy layer. TCP and UDP are
// counted apart, and a reader gets each and their sum.
//
// Every method does nothing on a nil *Counters, which is what a Registry
// returns for a key it does not count, so callers need no nil checks.
type Counters struct {
	tcpUplinkBytes    atomic.Uint64
	tcpDownlinkBytes  atomic.Uint64
	activeConnections atomic.Int64
	totalConnections  atomic.Uint64

	udpUplinkBytes     atomic.Uint64
	udpDownlinkBytes   atomic.Uint64
	udpUplinkPackets   atomic.Uint64
	udpDownlinkPackets atomic.Uint64
	udpSessions        atomic.Uint64
}

// TCP payload from the client toward the destination.
func (c *Counters) AddUplinkBytes(byteCount uint64) {
	if c != nil {
		c.tcpUplinkBytes.Add(byteCount)
	}
}

// TCP payload from the destination back to the client.
func (c *Counters) AddDownlinkBytes(byteCount uint64) {
	if c != nil {
		c.tcpDownlinkBytes.Add(byteCount)
	}
}

// A connection is counted once its handshake succeeded and routing let it
// through; a TCP connection that fails its handshake is only refused.
func (c *Counters) ConnectionStarted() {
	if c != nil {
		c.totalConnections.Add(1)
		c.activeConnections.Add(1)
	}
}

func (c *Counters) ConnectionEnded() {
	if c != nil {
		c.activeConnections.Add(-1)
	}
}

// One UDP datagram the outbound accepted, of size payload bytes, from the
// client toward the destination.
func (c *Counters) AddUplinkDatagram(size int) { c.AddUplinkDatagrams(uint64(size), 1) }

// One UDP datagram received from the outbound, toward the client.
func (c *Counters) AddDownlinkDatagram(size int) { c.AddDownlinkDatagrams(uint64(size), 1) }

// Several datagrams at once: a UDP session adds what it counted privately to
// the shared numbers in batches, so sessions do not fight over them.
func (c *Counters) AddUplinkDatagrams(byteCount, packetCount uint64) {
	if c != nil {
		c.udpUplinkBytes.Add(byteCount)
		c.udpUplinkPackets.Add(packetCount)
	}
}

func (c *Counters) AddDownlinkDatagrams(byteCount, packetCount uint64) {
	if c != nil {
		c.udpDownlinkBytes.Add(byteCount)
		c.udpDownlinkPackets.Add(packetCount)
	}
}

// A UDP session began, for a client key, or an outbound's datagram was opened
// for a session, for an outbound key.
func (c *Counters) UDPSessionStarted() {
	if c != nil {
		c.udpSessions.Add(1)
	}
}

// The zero Traffic for a nil *Counters.
func (c *Counters) Snapshot() Traffic {
	if c == nil {
		return Traffic{}
	}
	tcp := TCPTraffic{
		UplinkBytes:       c.tcpUplinkBytes.Load(),
		DownlinkBytes:     c.tcpDownlinkBytes.Load(),
		ActiveConnections: c.activeConnections.Load(),
		TotalConnections:  c.totalConnections.Load(),
	}
	udp := UDPTraffic{
		UplinkBytes:     c.udpUplinkBytes.Load(),
		DownlinkBytes:   c.udpDownlinkBytes.Load(),
		UplinkPackets:   c.udpUplinkPackets.Load(),
		DownlinkPackets: c.udpDownlinkPackets.Load(),
		Sessions:        c.udpSessions.Load(),
	}
	return Traffic{
		UplinkBytes:   tcp.UplinkBytes + udp.UplinkBytes,
		DownlinkBytes: tcp.DownlinkBytes + udp.DownlinkBytes,
		TCP:           tcp,
		UDP:           udp,
	}
}

// InboundCounters is an inbound's numbers: the base every key has, and what
// only an inbound sees. Methods do nothing on a nil *InboundCounters.
type InboundCounters struct {
	base                   Counters
	refusals               [refusalReasonCount]atomic.Uint64
	authenticationFailures atomic.Uint64
	fallbackConnections    atomic.Uint64
	datagramDrops          [inboundDatagramDropReasonCount]atomic.Uint64
}

// What the relay and the handler add bytes and connections to. Nil when
// the receiver is nil.
func (c *InboundCounters) Base() *Counters {
	if c == nil {
		return nil
	}
	return &c.base
}

// Refused: closed by core or by the protocol, with no outbound reached. The
// reason is one of the fixed list; one outside it counts as RefusalOther.
func (c *InboundCounters) ConnectionRefusedFor(reason RefusalReason) {
	if c == nil {
		return
	}
	if reason >= refusalReasonCount {
		reason = RefusalOther
	}
	c.refusals[reason].Add(1)
}

// A client spoke the protocol and its credentials were not accepted. Counted
// whatever happens to the connection next, so a guessing run shows even when
// every attempt is handed to the fallback.
func (c *InboundCounters) AuthenticationFailed() {
	if c != nil {
		c.authenticationFailures.Add(1)
	}
}

// A UDP datagram from a client was lost on the inbound's side. The reason is
// one of the fixed list; one outside it counts as InboundDropOther.
func (c *InboundCounters) DatagramDroppedFor(reason InboundDatagramDropReason) {
	if c == nil {
		return
	}
	if reason >= inboundDatagramDropReasonCount {
		reason = InboundDropOther
	}
	c.datagramDrops[reason].Add(1)
}

// Handed to the inbound's fallback. Only the connections are counted, not
// their bytes.
func (c *InboundCounters) ConnectionSentToFallback() {
	if c != nil {
		c.fallbackConnections.Add(1)
	}
}

func (c *InboundCounters) Snapshot() InboundTraffic {
	if c == nil {
		return InboundTraffic{}
	}
	var refusals [refusalReasonCount]uint64
	var refusedConnections uint64
	for reason := range c.refusals {
		refusals[reason] = c.refusals[reason].Load()
		refusedConnections += refusals[reason]
	}
	var drops [inboundDatagramDropReasonCount]uint64
	var datagramDrops uint64
	for reason := range c.datagramDrops {
		drops[reason] = c.datagramDrops[reason].Load()
		datagramDrops += drops[reason]
	}
	return InboundTraffic{
		Traffic:                c.base.Snapshot(),
		DatagramDrops:          datagramDrops,
		DatagramDropsByReason:  nonzeroByName(inboundDatagramDropReasonNames[:], drops[:]),
		RefusedConnections:     refusedConnections,
		RefusalsByReason:       nonzeroByName(refusalReasonNames[:], refusals[:]),
		AuthenticationFailures: c.authenticationFailures.Load(),
		FallbackConnections:    c.fallbackConnections.Load(),
	}
}

// OutboundCounters is an outbound's numbers: the base every key has, and what
// only an outbound sees. Methods do nothing on a nil *OutboundCounters.
type OutboundCounters struct {
	base          Counters
	dialFailures  [dialFailureReasonCount]atomic.Uint64
	datagramDrops [outboundDatagramDropReasonCount]atomic.Uint64

	// Only filled when the registry times dials.
	timedDials     atomic.Uint64
	totalDialNanos atomic.Int64
	lastDialNanos  atomic.Int64
}

// What the relay and the handler add bytes and connections to. Nil when
// the receiver is nil.
func (c *OutboundCounters) Base() *Counters {
	if c == nil {
		return nil
	}
	return &c.base
}

// The reason is one of the fixed list; one outside it counts as DialOther.
func (c *OutboundCounters) DialFailedFor(reason DialFailureReason) {
	if c == nil {
		return
	}
	if reason >= dialFailureReasonCount {
		reason = DialOther
	}
	c.dialFailures[reason].Add(1)
}

// A UDP datagram was lost on this outbound's side. The reason is one of the
// fixed list; one outside it counts as OutboundDropOther.
func (c *OutboundCounters) DatagramDroppedFor(reason OutboundDatagramDropReason) {
	if c == nil {
		return
	}
	if reason >= outboundDatagramDropReasonCount {
		reason = OutboundDropOther
	}
	c.datagramDrops[reason].Add(1)
}

// A dial that connected took this long. Only called when the registry times
// dials, so a default Apranik never reads the clock for it.
func (c *OutboundCounters) DialSucceededIn(took time.Duration) {
	if c == nil {
		return
	}
	c.timedDials.Add(1)
	c.totalDialNanos.Add(int64(took))
	c.lastDialNanos.Store(int64(took))
}

// IsGroup is left false: the Registry knows which outbounds are groups.
func (c *OutboundCounters) Snapshot() OutboundTraffic {
	if c == nil {
		return OutboundTraffic{}
	}
	var failures [dialFailureReasonCount]uint64
	var dialFailures uint64
	for reason := range c.dialFailures {
		failures[reason] = c.dialFailures[reason].Load()
		dialFailures += failures[reason]
	}
	var drops [outboundDatagramDropReasonCount]uint64
	var datagramDrops uint64
	for reason := range c.datagramDrops {
		drops[reason] = c.datagramDrops[reason].Load()
		datagramDrops += drops[reason]
	}
	return OutboundTraffic{
		Traffic:               c.base.Snapshot(),
		DatagramDrops:         datagramDrops,
		DatagramDropsByReason: nonzeroByName(outboundDatagramDropReasonNames[:], drops[:]),
		DialFailures:          dialFailures,
		DialFailuresByReason:  nonzeroByName(dialFailureReasonNames[:], failures[:]),
		TimedDials:            c.timedDials.Load(),
		TotalDialTime:         time.Duration(c.totalDialNanos.Load()),
		LastDialTime:          time.Duration(c.lastDialNanos.Load()),
	}
}
