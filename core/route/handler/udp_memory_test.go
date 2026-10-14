package handler

import (
	"net/netip"
	"runtime"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/shared/lru"
	"github.com/we-apranik/apranik/spec"
)

func heapInUse() uint64 {
	runtime.GC()
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return memory.HeapAlloc
}

func distinctDestinations(b *testing.B, count int) []spec.Destination {
	b.Helper()
	destinations := make([]spec.Destination, count)
	for index := range destinations {
		address := netip.AddrFrom4([4]byte{10, byte(index >> 16), byte(index >> 8), byte(index)})
		destination, err := spec.NewDestinationFromIP(address, 443)
		if err != nil {
			b.Fatal(err)
		}
		destinations[index] = destination
	}
	return destinations
}

// What one remembered routing decision holds on the heap, the cache's
// bookkeeping included, measured over many.
func BenchmarkRouteCacheEntryMemory(b *testing.B) {
	const entries = 10000
	destinations := distinctDestinations(b, entries)
	outbound := stubOutbound{}

	for range b.N {
		before := heapInUse()
		cache := lru.New[spec.Destination, *cachedRoute](entries)
		for _, destination := range destinations {
			route := &cachedRoute{decision: component.Decision{Outbound: outbound, Rule: "routing.rules[3]"}}
			route.uplinkAccepted = func(int) {}
			cache.Add(destination, route)
		}
		after := heapInUse()
		b.ReportMetric(float64(after-before)/entries, "bytes/entry")
		runtime.KeepAlive(cache)
	}
}

// What one listed UDP destination holds: the registry's connection, the live
// entry and the cache's bookkeeping.
func BenchmarkUDPLiveEntryMemory(b *testing.B) {
	const entries = 10000
	destinations := distinctDestinations(b, entries)

	for range b.N {
		registry := stats.NewRegistry(stats.Config{InboundTags: []string{"in"}, TrackLiveConversations: true})
		before := heapInUse()
		live := newUDPSessionLiveEntries(registry, stats.ConversationInfo{Network: spec.NetworkUDP, InboundTag: "in", User: "alice"},
			func(component.Outbound) []string { return []string{"auto", "member"} }, nil, time.Hour, entries)
		decision := component.Decision{Outbound: stubOutbound{}, Rule: "routing.rules[3]"}
		for _, destination := range destinations {
			live.uplinkAccepted(destination, decision, 100)
		}
		after := heapInUse()
		b.ReportMetric(float64(after-before)/entries, "bytes/entry")
		runtime.KeepAlive(live)
		runtime.KeepAlive(registry)
	}
}
