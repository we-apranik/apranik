package dns

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/The-Apranik/Apranik/core/blueprint"
)

func TestQueryAsksForExactlyTheRecordTypeGivenAndKeepsTheTTL(t *testing.T) {
	zone := dualStackZone()
	zone.ttl = 90
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)

	result, err := resolver.Query(context.Background(), "example.com", dnsmessage.TypeAAAA)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Addresses) != 1 || result.Addresses[0] != netip.MustParseAddr("2001:db8::1") {
		t.Errorf("addresses = %v, want the AAAA only, although ip_versions is ipv4_only", result.Addresses)
	}
	if result.TTL != 90*time.Second || result.ServerTag == "" {
		t.Errorf("ttl %v, server %q; want 90s and a server tag", result.TTL, result.ServerTag)
	}
}

func TestQueryUsesTheCache(t *testing.T) {
	zone := dualStackZone()
	resolver := newTestResolver(t, Config{}, zone)

	for range 2 {
		if _, err := resolver.Query(context.Background(), "example.com", dnsmessage.TypeA); err != nil {
			t.Fatal(err)
		}
	}
	if got := zone.queries.Load(); got != 1 {
		t.Errorf("%d queries for two identical Query calls, want 1", got)
	}
}

func TestQueryOfANameWithoutRecordsOfThatTypeFindsNoAddressesWithoutAnError(t *testing.T) {
	zone := newFakeZone()
	zone.addresses["v4only.example."] = addresses("192.0.2.9")
	resolver := newTestResolver(t, Config{}, zone)

	result, err := resolver.Query(context.Background(), "v4only.example", dnsmessage.TypeAAAA)
	if err != nil || len(result.Addresses) != 0 {
		t.Errorf("Query = %v, %v; want no addresses and no error", result.Addresses, err)
	}
}

func TestQueryRefusesWhatItDoesNotLookUp(t *testing.T) {
	resolver := newTestResolver(t, Config{}, dualStackZone())
	for name, call := range map[string]func() error{
		"a record type other than A and AAAA": func() error {
			_, err := resolver.Query(context.Background(), "example.com", dnsmessage.TypeMX)
			return err
		},
		"an address": func() error {
			_, err := resolver.Query(context.Background(), "192.0.2.1", dnsmessage.TypeA)
			return err
		},
		"an empty name": func() error {
			_, err := resolver.Query(context.Background(), "", dnsmessage.TypeA)
			return err
		},
		"a name with a byte a hostname cannot carry": func() error {
			_, err := resolver.Query(context.Background(), "exa mple.com", dnsmessage.TypeA)
			return err
		},
	} {
		if err := call(); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("%s: error = %v, want ErrInvalidQuery", name, err)
		}
	}
}

func TestQueryOfLocalhostAsksNoServer(t *testing.T) {
	zone := newFakeZone()
	resolver := newTestResolver(t, Config{}, zone)

	result, err := resolver.Query(context.Background(), "localhost", dnsmessage.TypeA)
	if err != nil || len(result.Addresses) != 1 || result.Addresses[0] != netip.MustParseAddr("127.0.0.1") || zone.queries.Load() != 0 {
		t.Errorf("Query = %+v, %v after %d queries; want 127.0.0.1 with no query", result, err, zone.queries.Load())
	}
}

func TestAFailedQueryMatchesErrLookupFailed(t *testing.T) {
	zone := newFakeZone()
	zone.failWith = errors.New("server unreachable")
	resolver := newTestResolver(t, Config{}, zone)

	if _, err := resolver.Query(context.Background(), "example.com", dnsmessage.TypeA); !errors.Is(err, ErrLookupFailed) {
		t.Errorf("error = %v, want ErrLookupFailed", err)
	}
}

func TestFlushCacheMakesTheNextLookupAskAgain(t *testing.T) {
	zone := dualStackZone()
	resolver := newTestResolver(t, Config{}, zone)
	lookup := func() {
		t.Helper()
		if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
			t.Fatal(err)
		}
	}
	lookup()
	lookup()
	queriesBefore := zone.queries.Load()

	resolver.FlushCache()
	lookup()
	if got := zone.queries.Load(); got <= queriesBefore {
		t.Errorf("%d queries after the flush, still %d: the cache was not emptied", got, queriesBefore)
	}
}

func TestFlushCacheEmptiesEveryServersCacheWhenThereAreSeveral(t *testing.T) {
	zone := dualStackZone()
	resolver := newTestResolver(t, Config{Cache: blueprint.DNSCache{PerServer: true}}, zone)
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	queriesBefore := zone.queries.Load()

	resolver.FlushCache()
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if zone.queries.Load() <= queriesBefore {
		t.Error("a per-server cache survived the flush")
	}
}

func TestFlushCacheOfAResolverWithNoCacheIsNotAnError(t *testing.T) {
	resolver := newTestResolver(t, Config{Cache: blueprint.DNSCache{Disable: true}}, dualStackZone())
	resolver.FlushCache()
}
