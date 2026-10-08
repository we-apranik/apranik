package dns

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/core/blueprint"
)

func TestServeStaleAnswersAtOnceAndRefreshes(t *testing.T) {
	zone := dualStackZone()
	zone.ttl = 10
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only,
		Cache: blueprint.DNSCache{ServeStale: true, MaxStaleAge: types.Pointer(time.Minute)}}, zone)
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	resolver.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(by time.Duration) { mu.Lock(); now = now.Add(by); mu.Unlock() }

	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	zone.addresses["example.com."] = addresses("192.0.2.99")
	advance(20 * time.Second)

	got, err := resolver.ResolveDomain(context.Background(), "example.com")
	if err != nil || !slices.Equal(got, addresses("192.0.2.1")) {
		t.Fatalf("stale lookup: got %v, %v; want the old answer at once", got, err)
	}
	waitFor(t, func() bool {
		got, _ := resolver.ResolveDomain(context.Background(), "example.com")
		return slices.Equal(got, addresses("192.0.2.99"))
	})

	advance(2 * time.Minute)
	zone.failWith = errFakeFailure
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); !errors.Is(err, errFakeFailure) {
		t.Fatalf("past max_stale_age: err = %v, want the server's failure", err)
	}
}

func TestAStaleAnswerIsCountedAndItsRefreshIsAQuery(t *testing.T) {
	zone := dualStackZone()
	zone.ttl = 10
	resolver, registry := countedResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only,
		Cache: blueprint.DNSCache{ServeStale: true, MaxStaleAge: types.Pointer(time.Minute)}}, zone)
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	resolver.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	now = now.Add(20 * time.Second)
	mu.Unlock()
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return registry.DNS().Servers["fake"].Queries == 2 })
	if got := registry.DNS(); got.StaleAnswers != 1 {
		t.Errorf("stale answers = %d, want 1", got.StaleAnswers)
	}
}
