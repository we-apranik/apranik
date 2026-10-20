package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/shared/lru"
	"github.com/we-apranik/apranik/spec"
)

// It lives apart from the routing cache: forgetting a routing decision does not end it, because the
// destination may still be sending and receiving.
type liveEntry struct {
	conversation       *stats.Conversation
	lastActiveUnixNano atomic.Int64
}

func (e *liveEntry) markActive() { e.lastActiveUnixNano.Store(time.Now().UnixNano()) }

func (e *liveEntry) idleDuration() time.Duration {
	return time.Since(time.Unix(0, e.lastActiveUnixNano.Load()))
}

// Its entries are bounded by maxEntries, so a client that sends to endless destinations costs a fixed
// amount of memory: when it is full, the entry used longest ago, in either direction, goes.
type udpSessionLiveEntries struct {
	registry *stats.Registry

	sessionInfo stats.ConversationInfo

	carryingOutboundTags func(routed component.Outbound) []string

	closeSession context.CancelFunc

	idleTimeout time.Duration

	mu      sync.Mutex
	entries *lru.Cache[spec.Destination, *liveEntry]
}

func newUDPSessionLiveEntries(registry *stats.Registry, sessionInfo stats.ConversationInfo, carryingOutboundTags func(routed component.Outbound) []string, closeSession context.CancelFunc, idleTimeout time.Duration, maxEntries int) *udpSessionLiveEntries {
	live := &udpSessionLiveEntries{
		registry:             registry,
		sessionInfo:          sessionInfo,
		carryingOutboundTags: carryingOutboundTags,
		closeSession:         closeSession,
		idleTimeout:          idleTimeout,
		entries:              lru.New[spec.Destination, *liveEntry](maxEntries),
	}
	return live
}

func (l *udpSessionLiveEntries) uplinkAccepted(destination spec.Destination, decision component.Decision, size int) {
	l.entryFor(destination, decision).conversation.Bytes().AddUplinkBytes(uint64(size))
}

func (l *udpSessionLiveEntries) downlinkReceived(destination spec.Destination, size int) (found bool) {
	l.mu.Lock()
	entry, found := l.entries.Get(destination)
	l.mu.Unlock()
	if !found {
		return false
	}
	entry.markActive()
	entry.conversation.Bytes().AddDownlinkBytes(uint64(size))
	return true
}

// A reply can be taken before the relay has told the entry that the datagram it
// answers was accepted, so a reply from a destination that is still routed
func (l *udpSessionLiveEntries) downlinkReceivedFromRoutedDestination(destination spec.Destination, decision component.Decision, size int) {
	l.entryFor(destination, decision).conversation.Bytes().AddDownlinkBytes(uint64(size))
}

func (l *udpSessionLiveEntries) entryFor(destination spec.Destination, decision component.Decision) *liveEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, found := l.entries.Get(destination)
	if !found {
		info := l.sessionInfo
		info.Destination = destination
		info.Rule = decision.Rule
		info.OutboundTags = l.carryingOutboundTags(decision.Outbound)
		entry = &liveEntry{conversation: l.registry.TrackConversation(info, l.closeSession)}
		if _, forgotten, wasFull := l.entries.Add(destination, entry); wasFull {
			forgotten.conversation.End()
		}
	}
	entry.markActive()
	return entry
}

func (l *udpSessionLiveEntries) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries.Each(func(_ spec.Destination, entry *liveEntry) bool {
		entry.conversation.End()
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
		oldest.conversation.End()
	}
}
