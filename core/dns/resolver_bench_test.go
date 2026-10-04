package dns

import (
	"context"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/The-Apranik/Apranik/core/blueprint"
)

// A cache hit is what nearly every dial of a domain costs.
func BenchmarkResolveCachedOneRecordType(b *testing.B) {
	resolver := newTestResolver(b, Config{IPVersions: blueprint.IPVersionsIPv4Only}, dualStackZone())
	ctx := context.Background()
	resolver.ResolveDomain(ctx, "example.com")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := resolver.ResolveDomain(ctx, "example.com"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveCachedTwoRecordTypes(b *testing.B) {
	resolver := newTestResolver(b, Config{IPVersions: blueprint.IPVersionsPreferIPv4}, dualStackZone())
	ctx := context.Background()
	resolver.ResolveDomain(ctx, "example.com")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := resolver.ResolveDomain(ctx, "example.com"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveCachedParallel(b *testing.B) {
	resolver := newTestResolver(b, Config{IPVersions: blueprint.IPVersionsIPv4Only}, dualStackZone())
	ctx := context.Background()
	resolver.ResolveDomain(ctx, "example.com")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resolver.ResolveDomain(ctx, "example.com")
		}
	})
}

// A miss: query built, answered by an in-memory server, read and cached.
func BenchmarkResolveUncached(b *testing.B) {
	zone := dualStackZone()
	resolver := newTestResolver(b, Config{IPVersions: blueprint.IPVersionsIPv4Only, Cache: blueprint.DNSCache{Disable: true}}, zone)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := resolver.ResolveDomain(ctx, "example.com"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadAnswer(b *testing.B) {
	zone := dualStackZone()
	name := mustName("example.com")
	response := zone.respond(7, dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := readAnswer(response, 7, name, dnsmessage.TypeA); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCacheStoreWithEviction(b *testing.B) {
	cache := newAnswerCache(DefaultCacheMaxEntries, 0)
	stored := answer{addresses: addresses("192.0.2.1"), ttl: time.Minute}
	keys := make([]cacheKey, 2*DefaultCacheMaxEntries)
	for index := range keys {
		keys[index] = cacheKey{name: "host" + strconv.Itoa(index) + ".example"}
	}
	now := testNow()
	b.ReportAllocs()
	index := 0
	for b.Loop() {
		cache.store(keys[index%len(keys)], stored, now)
		index++
	}
}

func testNow() time.Time { return time.Unix(1000, 0) }
