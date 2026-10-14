package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/shared/lru"
	"github.com/we-apranik/apranik/spec"
)

type uncachedRouteFunc func(ctx context.Context, destination spec.Destination) (component.Decision, error)

type cachedRoute struct {
	decision           component.Decision
	lastActiveUnixNano atomic.Int64

	// Called with the size of each datagram the outbound accepted for this
	// destination. Nil when live entries are not tracked, or the decision is a block.
	uplinkAccepted func(size int)

	// Whether a datagram was ever accepted for this destination.
	uplinkReported atomic.Bool
}

func (c *cachedRoute) markActive() { c.lastActiveUnixNano.Store(time.Now().UnixNano()) }

func (c *cachedRoute) idleDuration() time.Duration {
	return time.Since(time.Unix(0, c.lastActiveUnixNano.Load()))
}

// One per UDP session. Everything in the session's Metadata but the
// destination is fixed, so the destination alone is a correct key.
//
// A route is forgotten when its destination has been idle for idleTimeout, or
// when the cache is full and a new destination needs its place: the route used
// longest ago goes. A forgotten destination is simply routed again, so the
// limit only costs a client that uses more destinations at once than it allows.
// A route is used by a packet to its destination; a reply does not use it.
// Forgetting a route only forgets the decision: the destination's live entry
// is kept by the session's live entries, which end it by their own rules.
type udpSessionRoutingCache struct {
	routeWithoutCache uncachedRouteFunc
	idleTimeout       time.Duration

	// Given a destination that was routed to an outbound, makes what the relay
	// calls when the outbound accepted a datagram for it. Nil when live entries
	// are not tracked. Set before the first route.
	newUplinkAccepted func(destination spec.Destination, decision component.Decision) func(size int)

	mu                 sync.Mutex
	routeByDestination *lru.Cache[spec.Destination, *cachedRoute]
	routingInProgress  map[spec.Destination]chan struct{}
}

func newUDPSessionRoutingCache(routeWithoutCache uncachedRouteFunc, idleTimeout time.Duration, maxEntries int) *udpSessionRoutingCache {
	cache := &udpSessionRoutingCache{
		routeWithoutCache:  routeWithoutCache,
		idleTimeout:        idleTimeout,
		routeByDestination: lru.New[spec.Destination, *cachedRoute](maxEntries),
		routingInProgress:  make(map[spec.Destination]chan struct{}),
	}
	return cache
}

// Forgets every route: the session is over.
func (c *udpSessionRoutingCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routeByDestination.Clear()
}

// Racing first packets to one destination wait for a single routing. A block
// is cached like any decision; a failure is not, so a transient one is not
// pinned for the idle window.
func (c *udpSessionRoutingCache) route(ctx context.Context, destination spec.Destination) (*cachedRoute, error) {
	for {
		c.mu.Lock()
		if cached, found := c.routeByDestination.Get(destination); found {
			// A decision never outlives its idle time, even before the sweeper
			// has come round to it.
			if cached.idleDuration() < c.idleTimeout {
				c.mu.Unlock()
				cached.markActive()
				return cached, nil
			}
			c.routeByDestination.Remove(destination)
		}
		if inProgressRouting, found := c.routingInProgress[destination]; found {
			c.mu.Unlock()
			<-inProgressRouting
			continue
		}
		routingFinished := make(chan struct{})
		c.routingInProgress[destination] = routingFinished
		c.mu.Unlock()

		decision, err := c.routeWithoutCache(ctx, destination)

		var newRoute *cachedRoute
		c.mu.Lock()
		delete(c.routingInProgress, destination)
		if err == nil {
			newRoute = &cachedRoute{decision: decision}
			if c.newUplinkAccepted != nil && !decision.IsBlock() {
				report := c.newUplinkAccepted(destination, decision)
				newRoute.uplinkAccepted = func(size int) {
					if !newRoute.uplinkReported.Load() {
						newRoute.uplinkReported.Store(true)
					}
					report(size)
				}
			}
			newRoute.markActive()
			c.routeByDestination.Add(destination, newRoute)
		}
		c.mu.Unlock()
		close(routingFinished)

		return newRoute, err
	}
}

// The decision remembered for this destination, without using the route, but
// only while no datagram was ever accepted for it: a reply can beat the relay's
// report of the datagram it answers, and only then may it list the destination.
// A destination listed before and since ended is not listed again by a reply,
// or a sender spraying replies could keep making and evicting entries.
func (c *udpSessionRoutingCache) decisionAwaitingFirstUplinkReport(destination spec.Destination) (component.Decision, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, found := c.routeByDestination.Peek(destination)
	if !found || cached.uplinkReported.Load() {
		return component.Decision{}, false
	}
	return cached.decision, true
}

// Routes are ordered by their last use, so the sweep stops at the first one
// that is not idle and costs what it removes, not what the cache holds.
func (c *udpSessionRoutingCache) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for {
		_, oldest, found := c.routeByDestination.Oldest()
		if !found || oldest.idleDuration() < c.idleTimeout {
			return
		}
		c.routeByDestination.RemoveOldest()
	}
}
