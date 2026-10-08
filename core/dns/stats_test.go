package dns

import (
	"context"
	"errors"
	"maps"
	"net"
	"os"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

func countedResolver(t *testing.T, config Config, zone *fakeZone) (*Resolver, *stats.Registry) {
	t.Helper()
	registry := stats.NewRegistry(stats.Config{DNSServerTags: []string{"fake"}})
	resolver := newTestResolver(t, config, zone)
	resolver.SetStats(registry.DNSCounters())
	return resolver, registry
}

func TestLookupsCacheHitsAndQueriesAreCounted(t *testing.T) {
	resolver, registry := countedResolver(t, Config{}, dualStackZone())

	for range 2 {
		if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
			t.Fatal(err)
		}
	}

	got := registry.DNS()
	if got.Lookups != 2 {
		t.Errorf("lookups = %d, want 2", got.Lookups)
	}
	if got.CacheHits != 2 {
		t.Errorf("cache hits = %d, want 2: the second lookup answers both record types from the cache", got.CacheHits)
	}
	server := got.Servers["fake"]
	if server.Queries != 2 || server.Failures != 0 {
		t.Errorf("server fake = %+v, want 2 queries (A and AAAA, once) and no failure", server)
	}
	if got.Failures != 0 || got.FailuresByReason != nil {
		t.Errorf("failures = %d %v, want none", got.Failures, got.FailuresByReason)
	}
}

func TestAnAddressLiteralAndLocalhostAreNotLookups(t *testing.T) {
	resolver, registry := countedResolver(t, Config{}, dualStackZone())

	for _, name := range []string{"192.0.2.1", "2001:db8::1", "localhost"} {
		if _, err := resolver.ResolveDomain(context.Background(), name); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	if got := registry.DNS(); got.Lookups != 0 || got.Servers["fake"].Queries != 0 {
		t.Errorf("dns = %+v, want nothing counted: none of these reaches a server", got)
	}
}

func TestAServerQueryRecordsItsLatency(t *testing.T) {
	zone := dualStackZone()
	zone.release = make(chan struct{})
	resolver, registry := countedResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)

	done := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveDomain(context.Background(), "example.com")
		done <- err
	}()
	time.Sleep(60 * time.Millisecond)
	close(zone.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if got := registry.DNS().Servers["fake"]; got.Queries != 1 || got.TotalLatency < 50*time.Millisecond {
		t.Errorf("server fake = %+v, want one query of at least 50ms", got)
	}
}

func TestEachKindOfFailureIsCountedUnderItsReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		zone func() *fakeZone
		want string
	}{
		{"a name that does not exist", newFakeZone, "no_answer"},
		{"a server timeout", func() *fakeZone {
			zone := dualStackZone()
			zone.failWith = &net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}
			return zone
		}, "timeout"},
		{"a truncated answer", func() *fakeZone {
			zone := dualStackZone()
			zone.failWith = spec.ErrDNSAnswerTruncated
			return zone
		}, "truncated"},
		{"an error response code", func() *fakeZone {
			zone := dualStackZone()
			zone.failWith = &ServerFailureError{}
			return zone
		}, "server_failed"},
		{"anything else", func() *fakeZone {
			zone := dualStackZone()
			zone.failWith = errFakeFailure
			return zone
		}, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver, registry := countedResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, tc.zone())

			_, err := resolver.ResolveDomain(context.Background(), "example.com")
			if err == nil {
				t.Fatal("the lookup succeeded")
			}

			got := registry.DNS()
			if !maps.Equal(got.FailuresByReason, map[string]uint64{tc.want: 1}) || got.Failures != 1 {
				t.Errorf("failures = %d %v, want one %s", got.Failures, got.FailuresByReason, tc.want)
			}
		})
	}
}

func TestAServerThatFailsIsCountedOnThatServer(t *testing.T) {
	zone := dualStackZone()
	zone.failWith = errFakeFailure
	resolver, registry := countedResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)

	resolver.ResolveDomain(context.Background(), "example.com")

	if got := registry.DNS().Servers["fake"]; got.Queries != 1 || got.Failures != 1 {
		t.Errorf("server fake = %+v, want 1 query and 1 failure", got)
	}
}

func TestACountedResolverWithoutCountersChangesNothing(t *testing.T) {
	resolver := newTestResolver(t, Config{}, dualStackZone())
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	resolver.SetStats(nil)
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnknownServerTagIsNotCounted(t *testing.T) {
	registry := stats.NewRegistry(stats.Config{DNSServerTags: []string{"another"}})
	resolver := newTestResolver(t, Config{}, dualStackZone())
	resolver.SetStats(registry.DNSCounters())

	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := registry.DNS(); got.Lookups != 1 || got.Servers["another"].Queries != 0 {
		t.Errorf("dns = %+v, want the lookup counted and the unnamed server's queries not", got)
	}
}

func TestEveryLookupFailureHasANameInTheErrorChain(t *testing.T) {
	for _, err := range []error{
		&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded},
		errors.Join(errFakeFailure, spec.ErrDNSAnswerTruncated),
		&ServerFailureError{},
		ErrNoAddresses,
		errFakeFailure,
	} {
		if lookupFailureReason(err).String() == "" {
			t.Errorf("lookupFailureReason(%v) has no name", err)
		}
	}
}
