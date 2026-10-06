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
	return newBoundedRoutingCacheForTest(t, idleTimeout, 1024, routeWithoutCache)
}

func newBoundedRoutingCacheForTest(t *testing.T, idleTimeout time.Duration, maxEntries int, routeWithoutCache uncachedRouteFunc) (*udpSessionRoutingCache, *atomic.Int32) {
	t.Helper()

	var routerCalls atomic.Int32
	cache := newUDPSessionRoutingCache(func(ctx context.Context, destination spec.Destination) (component.Decision, error) {
		routerCalls.Add(1)
		return routeWithoutCache(ctx, destination)
	}, idleTimeout, maxEntries)
	sweeper := newPeriodicSweeper(sweepIntervalFor(idleTimeout))
	sweeper.add(cache)
	t.Cleanup(func() {
		sweeper.remove(cache)
		cache.close()
	})
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

func TestAnIdleRouteIsForgottenWithinAQuarterOfTheIdleTimeAfterItBecameIdle(t *testing.T) {
	const idleTimeout = 400 * time.Millisecond
	cache, _ := newRoutingCacheForTest(t, idleTimeout, func(context.Context, spec.Destination) (component.Decision, error) {
		return component.Decision{Outbound: stubOutbound{}}, nil
	})

	cache.route(context.Background(), destinationForTest(t, "192.0.2.1", 53))
	forgottenBy := time.Now().Add(idleTimeout + idleTimeout/4 + 150*time.Millisecond)
	for time.Now().Before(forgottenBy) {
		cache.mu.Lock()
		remembered := cache.routeByDestination.Len()
		cache.mu.Unlock()
		if remembered == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the route was still remembered %s after its last use, want it gone within %s", time.Since(forgottenBy)+idleTimeout+idleTimeout/4+150*time.Millisecond, idleTimeout+idleTimeout/4)
}

func TestTheSweepIntervalIsAQuarterOfTheIdleTimeButNeverZero(t *testing.T) {
	if got := sweepIntervalFor(time.Minute); got != 15*time.Second {
		t.Errorf("sweep interval for a minute = %s, want 15s", got)
	}
	if got := sweepIntervalFor(time.Microsecond); got != time.Millisecond {
		t.Errorf("sweep interval for a microsecond = %s, want the 1ms floor: a ticker needs a positive interval", got)
	}
}

func BenchmarkCachedRouteHit(b *testing.B) {
	cache := newUDPSessionRoutingCache(func(context.Context, spec.Destination) (component.Decision, error) {
		return component.Decision{Outbound: stubOutbound{}}, nil
	}, time.Minute, 1024)
	b.Cleanup(cache.close)
	target, err := spec.NewDestinationFromIP(netip.MustParseAddr("192.0.2.1"), 53)
	if err != nil {
		b.Fatal(err)
	}
	cache.route(context.Background(), target)

	b.ReportAllocs()
	for b.Loop() {
		cache.route(context.Background(), target)
	}
}

func BenchmarkCachedRouteHitParallel(b *testing.B) {
	cache := newUDPSessionRoutingCache(func(context.Context, spec.Destination) (component.Decision, error) {
		return component.Decision{Outbound: stubOutbound{}}, nil
	}, time.Minute, 1024)
	b.Cleanup(cache.close)
	target, err := spec.NewDestinationFromIP(netip.MustParseAddr("192.0.2.1"), 53)
	if err != nil {
		b.Fatal(err)
	}
	cache.route(context.Background(), target)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cache.route(context.Background(), target)
		}
	})
}

func routeToStub(context.Context, spec.Destination) (component.Decision, error) {
	return component.Decision{Outbound: stubOutbound{}}, nil
}

func TestAFullCacheForgetsTheDestinationUsedLongestAgo(t *testing.T) {
	cache, routerCalls := newBoundedRoutingCacheForTest(t, time.Minute, 2, routeToStub)
	first, second, third := destinationForTest(t, "192.0.2.1", 53), destinationForTest(t, "192.0.2.2", 53), destinationForTest(t, "192.0.2.3", 53)
	ctx := context.Background()

	cache.route(ctx, first)
	cache.route(ctx, second)
	cache.route(ctx, first) // first is used again, so second is now the oldest
	cache.route(ctx, third)

	if got := routerCalls.Load(); got != 3 {
		t.Fatalf("routed %d times, want 3", got)
	}
	cache.route(ctx, first)
	cache.route(ctx, third)
	if got := routerCalls.Load(); got != 3 {
		t.Errorf("routed %d times, want 3: first and third are still remembered", got)
	}
	cache.route(ctx, second)
	if got := routerCalls.Load(); got != 4 {
		t.Errorf("routed %d times, want 4: second was forgotten and is routed again", got)
	}
}

func TestACacheNeverHoldsMoreThanItsLimitHoweverManyDestinationsArrive(t *testing.T) {
	cache, routerCalls := newBoundedRoutingCacheForTest(t, time.Minute, 50, routeToStub)
	for host := range 5000 {
		cache.route(context.Background(), destinationForTest(t, "10.1.0.0", uint16(host+1)))
		if remembered := cache.routeByDestination.Len(); remembered > 50 {
			t.Fatalf("%d routes remembered after %d destinations, limit 50", remembered, host+1)
		}
	}
	if got := routerCalls.Load(); got != 5000 {
		t.Errorf("routed %d times for 5000 distinct destinations, want 5000: every one is routed", got)
	}
}

func TestAForgottenDestinationIsRoutedAgainSoItIsNeverWronglyAllowed(t *testing.T) {
	blocked := destinationForTest(t, "192.0.2.99", 9)
	cache, _ := newBoundedRoutingCacheForTest(t, time.Minute, 1, func(_ context.Context, destination spec.Destination) (component.Decision, error) {
		if destination == blocked {
			return component.Decision{}, nil // a block: no outbound
		}
		return component.Decision{Outbound: stubOutbound{}}, nil
	})
	ctx := context.Background()

	first, _ := cache.route(ctx, blocked)
	cache.route(ctx, destinationForTest(t, "192.0.2.1", 53)) // pushes the blocked one out
	again, _ := cache.route(ctx, blocked)

	if !first.decision.IsBlock() || !again.decision.IsBlock() {
		t.Errorf("a blocked destination was allowed after it was forgotten: %+v then %+v", first.decision, again.decision)
	}
}

func TestTheSweepRemovesOnlyTheIdleRoutesFromTheOldestEnd(t *testing.T) {
	cache, _ := newBoundedRoutingCacheForTest(t, time.Hour, 10, routeToStub)
	ctx := context.Background()
	old, recent := destinationForTest(t, "192.0.2.1", 53), destinationForTest(t, "192.0.2.2", 53)
	oldRoute, _ := cache.route(ctx, old)
	cache.route(ctx, recent)
	oldRoute.lastActiveUnixNano.Store(time.Now().Add(-2 * time.Hour).UnixNano())

	cache.sweep()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, found := cache.routeByDestination.Peek(old); found {
		t.Error("the idle route was not removed")
	}
	if _, found := cache.routeByDestination.Peek(recent); !found {
		t.Error("the recent route was removed")
	}
}

func TestADecisionNeverOutlivesItsIdleTimeEvenBeforeTheSweeperRuns(t *testing.T) {
	cache := newUDPSessionRoutingCache(routeToStub, 50*time.Millisecond, 10) // no sweeper
	target := destinationForTest(t, "192.0.2.1", 53)
	first, _ := cache.route(context.Background(), target)

	time.Sleep(120 * time.Millisecond)
	second, _ := cache.route(context.Background(), target)

	if first == second {
		t.Error("an idle decision was used again: it should have been forgotten when it went idle")
	}
}

func TestOnlyADestinationNoDatagramWasAcceptedForMayBeListedByAReply(t *testing.T) {
	cache, _ := newRoutingCacheForTest(t, time.Minute, routeToStub)
	cache.newUplinkAccepted = func(spec.Destination, component.Decision) func(size int) { return func(int) {} }
	target := destinationForTest(t, "192.0.2.1", 53)

	route, _ := cache.route(context.Background(), target)
	if _, awaiting := cache.decisionAwaitingFirstUplinkReport(target); !awaiting {
		t.Fatal("a routed destination with no datagram accepted yet was not open to a reply")
	}
	route.uplinkAccepted(10)
	if _, awaiting := cache.decisionAwaitingFirstUplinkReport(target); awaiting {
		t.Error("a destination whose datagram was accepted is still open to a reply: a spray could relist it")
	}
	if _, awaiting := cache.decisionAwaitingFirstUplinkReport(destinationForTest(t, "192.0.2.2", 53)); awaiting {
		t.Error("a destination never routed is open to a reply")
	}
}
