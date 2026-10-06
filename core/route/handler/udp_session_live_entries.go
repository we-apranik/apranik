package handler

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/shared/lru"
	"github.com/The-Apranik/Apranik/spec"
)

// One live entry for one destination of one UDP session: listed in the
// registry from the destination's first accepted datagram or reply until it
// has been silent for the idle time, the session lists too many and it is the
// one used longest ago, or the session ends. It lives apart from the routing
// cache: forgetting a routing decision does not end it, because the
// destination may still be sending and receiving.
type liveEntry struct {
	connection         *stats.Connection
	lastActiveUnixNano atomic.Int64
}

func (e *liveEntry) markActive() { e.lastActiveUnixNano.Store(time.Now().UnixNano()) }

func (e *liveEntry) idleDuration() time.Duration {
	return time.Since(time.Unix(0, e.lastActiveUnixNano.Load()))
}

// One per UDP session. Its entries are bounded by maxEntries, so a client that
// sends to endless destinations costs a fixed amount of memory: when it is
// full, the entry used longest ago, in either direction, goes.
type udpSessionLiveEntries struct {
	registry *stats.Registry

	// What every entry of the session shares; each adds its destination, rule
	// and outbounds.
	sessionInfo stats.ConnectionInfo

	// The outbounds that carried the session's datagram through the routed
	// outbound, known once something was sent through it.
	carryingOutboundTags func(routed component.Outbound) []string

	idleTimeout time.Duration

	mu      sync.Mutex
	entries *lru.Cache[spec.Destination, *liveEntry]
}

func newUDPSessionLiveEntries(registry *stats.Registry, sessionInfo stats.ConnectionInfo, carryingOutboundTags func(routed component.Outbound) []string, idleTimeout time.Duration, maxEntries int) *udpSessionLiveEntries {
	live := &udpSessionLiveEntries{
		registry:             registry,
		sessionInfo:          sessionInfo,
		carryingOutboundTags: carryingOutboundTags,
		idleTimeout:          idleTimeout,
		entries:              lru.New[spec.Destination, *liveEntry](maxEntries),
	}
	return live
}

// A datagram of size bytes was accepted by the outbound for destination, which
// the decision routed.
func (l *udpSessionLiveEntries) uplinkAccepted(destination spec.Destination, decision component.Decision, size int) {
	l.entryFor(destination, decision).connection.Bytes().AddUplinkBytes(uint64(size))
}

// A reply of size bytes arrived from destination. It counts on the destination's
// entry when there is one; found says whether there was.
func (l *udpSessionLiveEntries) downlinkReceived(destination spec.Destination, size int) (found bool) {
	l.mu.Lock()
	entry, found := l.entries.Get(destination)
	l.mu.Unlock()
	if !found {
		return false
	}
	entry.markActive()
	entry.connection.Bytes().AddDownlinkBytes(uint64(size))
	return true
}

// A reply can be taken before the relay has told the entry that the datagram it
// answers was accepted, so a reply from a destination that is still routed
// lists its entry too.
func (l *udpSessionLiveEntries) downlinkReceivedFromRoutedDestination(destination spec.Destination, decision component.Decision, size int) {
	l.entryFor(destination, decision).connection.Bytes().AddDownlinkBytes(uint64(size))
}

// The destination's entry, made when there is none, and used now.
func (l *udpSessionLiveEntries) entryFor(destination spec.Destination, decision component.Decision) *liveEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, found := l.entries.Get(destination)
	if !found {
		info := l.sessionInfo
		info.Destination = destination
		info.Rule = decision.Rule
		info.OutboundTags = l.carryingOutboundTags(decision.Outbound)
		entry = &liveEntry{connection: l.registry.TrackConnection(info)}
		if _, forgotten, wasFull := l.entries.Add(destination, entry); wasFull {
			forgotten.connection.End()
		}
	}
	entry.markActive()
	return entry
}

// Ends every entry: the session is over.
func (l *udpSessionLiveEntries) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries.Each(func(_ spec.Destination, entry *liveEntry) bool {
		entry.connection.End()
		return true
	})
	l.entries.Clear()
}

// Entries are ordered by their last use, so the sweep stops at the first that
// is not idle and costs what it removes, not what the session lists.
func (l *udpSessionLiveEntries) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for {
		_, oldest, found := l.entries.Oldest()
		if !found || oldest.idleDuration() < l.idleTimeout {
			return
		}
		l.entries.RemoveOldest()
		oldest.connection.End()
	}
}
