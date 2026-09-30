package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/spec"
)

// PROVISIONAL: 1 minute.
const cachedRouteIdleTimeout = time.Minute

type uncachedRouteFunc func(ctx context.Context, destination spec.Destination) (component.Decision, error)

type cachedRoute struct {
	decision           component.Decision
	lastActiveUnixNano atomic.Int64
}

func (c *cachedRoute) markActive() { c.lastActiveUnixNano.Store(time.Now().UnixNano()) }

func (c *cachedRoute) idleDuration() time.Duration {
	return time.Since(time.Unix(0, c.lastActiveUnixNano.Load()))
}

// One per UDP session. Everything in the session's Metadata but the
// destination is fixed, so the destination alone is a correct key.
type udpSessionRoutingCache struct {
	routeWithoutCache uncachedRouteFunc
	idleTimeout       time.Duration

	mu                 sync.Mutex
	routeByDestination map[spec.Destination]*cachedRoute
	routingInProgress  map[spec.Destination]chan struct{}

	stopEviction     chan struct{}
	stopEvictionOnce sync.Once
}

func newUDPSessionRoutingCache(routeWithoutCache uncachedRouteFunc, idleTimeout time.Duration) *udpSessionRoutingCache {
	cache := &udpSessionRoutingCache{
		routeWithoutCache:  routeWithoutCache,
		idleTimeout:        idleTimeout,
		routeByDestination: make(map[spec.Destination]*cachedRoute),
		routingInProgress:  make(map[spec.Destination]chan struct{}),
		stopEviction:       make(chan struct{}),
	}
	go cache.evictIdleRoutesUntilClosed()
	return cache
}

func (c *udpSessionRoutingCache) close() {
	c.stopEvictionOnce.Do(func() { close(c.stopEviction) })
}

// Racing first packets to one destination wait for a single routing. A block
// is cached like any decision; a failure is not, so a transient one is not
// pinned for the idle window.
func (c *udpSessionRoutingCache) route(ctx context.Context, destination spec.Destination) (component.Decision, error) {
	for {
		c.mu.Lock()
		if cached, found := c.routeByDestination[destination]; found {
			c.mu.Unlock()
			cached.markActive()
			return cached.decision, nil
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

		c.mu.Lock()
		delete(c.routingInProgress, destination)
		if err == nil {
			cached := &cachedRoute{decision: decision}
			cached.markActive()
			c.routeByDestination[destination] = cached
		}
		c.mu.Unlock()
		close(routingFinished)

		return decision, err
	}
}

func (c *udpSessionRoutingCache) evictIdleRoutesUntilClosed() {
	sweepTicker := time.NewTicker(c.idleTimeout)
	defer sweepTicker.Stop()

	for {
		select {
		case <-sweepTicker.C:
			c.evictIdleRoutes()
		case <-c.stopEviction:
			return
		}
	}
}

func (c *udpSessionRoutingCache) evictIdleRoutes() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for destination, cached := range c.routeByDestination {
		if cached.idleDuration() >= c.idleTimeout {
			delete(c.routeByDestination, destination)
		}
	}
}
