package handler

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/spec"
)

type stubOutbound struct{ component.Outbound }

func destinationForTest(t *testing.T, host string, port uint16) spec.Destination {
	t.Helper()

	made, err := spec.NewDestinationFromIP(netip.MustParseAddr(host), port)
	if err != nil {
		t.Fatalf("NewDestinationFromIP: %v", err)
	}
	return made
}

func newRoutingCacheForTest(t *testing.T, idleTimeout time.Duration, routeWithoutCache uncachedRouteFunc) (*udpSessionRoutingCache, *atomic.Int32) {
	t.Helper()

	var routerCalls atomic.Int32
	cache := newUDPSessionRoutingCache(func(ctx context.Context, destination spec.Destination) (component.Decision, error) {
		routerCalls.Add(1)
		return routeWithoutCache(ctx, destination)
	}, idleTimeout)
	t.Cleanup(cache.close)
	return cache, &routerCalls
}

func TestEachDestinationIsRoutedOnce(t *testing.T) {
	outbound := stubOutbound{}
	cache, routerCalls := newRoutingCacheForTest(t, time.Minute, func(context.Context, spec.Destination) (component.Decision, error) {
		return component.Decision{Outbound: outbound}, nil
	})

	first := destinationForTest(t, "192.0.2.1", 53)
	second := destinationForTest(t, "192.0.2.2", 443)
	for range 5 {
		cache.route(context.Background(), first)
		cache.route(context.Background(), second)
	}

	if got := routerCalls.Load(); got != 2 {
		t.Errorf("routed %d times for 2 destinations over 10 lookups, want 2", got)
	}
}

func TestRacingFirstLookupsShareOneRouting(t *testing.T) {
	cache, routerCalls := newRoutingCacheForTest(t, time.Minute, func(context.Context, spec.Destination) (component.Decision, error) {
		time.Sleep(30 * time.Millisecond)
		return component.Decision{Outbound: stubOutbound{}}, nil
	})

	target := destinationForTest(t, "192.0.2.1", 53)
	var lookups sync.WaitGroup
	for range 8 {
		lookups.Go(func() { cache.route(context.Background(), target) })
	}
	lookups.Wait()

	if got := routerCalls.Load(); got != 1 {
		t.Errorf("routed %d times for one destination, want 1", got)
	}
}

func TestAFailedRoutingIsNotCached(t *testing.T) {
	refused := errors.New("refused")
	cache, routerCalls := newRoutingCacheForTest(t, time.Minute, func(context.Context, spec.Destination) (component.Decision, error) {
		return component.Decision{}, refused
	})

	target := destinationForTest(t, "192.0.2.1", 53)
	for range 3 {
		if _, err := cache.route(context.Background(), target); !errors.Is(err, refused) {
			t.Fatalf("err = %v, want %v", err, refused)
		}
	}

	if got := routerCalls.Load(); got != 3 {
		t.Errorf("routed %d times, want 3", got)
	}
}

func TestAnIdleDestinationIsRoutedAgain(t *testing.T) {
	cache, routerCalls := newRoutingCacheForTest(t, 50*time.Millisecond, func(context.Context, spec.Destination) (component.Decision, error) {
		return component.Decision{Outbound: stubOutbound{}}, nil
	})

	target := destinationForTest(t, "192.0.2.1", 53)
	cache.route(context.Background(), target)
	time.Sleep(200 * time.Millisecond)
	cache.route(context.Background(), target)

	if got := routerCalls.Load(); got != 2 {
		t.Errorf("routed %d times, want 2: the idle entry should have been evicted", got)
	}
}
