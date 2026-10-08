package dns

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/core/route/router/match"
)

func addresses(texts ...string) []netip.Addr {
	parsed := make([]netip.Addr, len(texts))
	for index, text := range texts {
		parsed[index] = netip.MustParseAddr(text)
	}
	return parsed
}

func dualStackZone() *fakeZone {
	zone := newFakeZone()
	zone.addresses["example.com."] = addresses("192.0.2.1", "2001:db8::1")
	return zone
}

func TestResolverOrdersAddressesByIPVersions(t *testing.T) {
	for _, tc := range []struct {
		ipVersions blueprint.IPVersions
		want       []netip.Addr
	}{
		{blueprint.IPVersionsIPv4Only, addresses("192.0.2.1")},
		{blueprint.IPVersionsIPv6Only, addresses("2001:db8::1")},
		{blueprint.IPVersionsPreferIPv4, addresses("192.0.2.1", "2001:db8::1")},
		{blueprint.IPVersionsPreferIPv6, addresses("2001:db8::1", "192.0.2.1")},
		{"", addresses("192.0.2.1", "2001:db8::1")},
	} {
		t.Run(string(tc.ipVersions), func(t *testing.T) {
			resolver := newTestResolver(t, Config{IPVersions: tc.ipVersions}, dualStackZone())
			got, err := resolver.ResolveDomain(context.Background(), "Example.COM.")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServerIPVersionsOverrideTheResolvers(t *testing.T) {
	resolver := NewResolver(Config{IPVersions: blueprint.IPVersionsPreferIPv4})
	if err := resolver.AttachServers([]Server{{Tag: "fake", IPVersions: blueprint.IPVersionsIPv6Only, Wire: dualStackZone()}}, nil, "fake", nil); err != nil {
		t.Fatal(err)
	}
	defer resolver.Close()
	got, err := resolver.ResolveDomain(context.Background(), "example.com")
	if err != nil || !slices.Equal(got, addresses("2001:db8::1")) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestResolverAsksOnlyForTheRecordTypesItNeeds(t *testing.T) {
	zone := dualStackZone()
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if zone.queries.Load() != 1 || zone.questions[0].Type != dnsmessage.TypeA {
		t.Fatalf("questions %+v", zone.questions)
	}
}

func TestResolverCachesUntilTheTTLEnds(t *testing.T) {
	zone := dualStackZone()
	zone.ttl = 60
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)
	now := time.Unix(1000, 0)
	resolver.now = func() time.Time { return now }

	resolve := func() {
		t.Helper()
		if _, err := resolver.ResolveDomain(context.Background(), "example.com"); err != nil {
			t.Fatal(err)
		}
	}
	resolve()
	resolve()
	if got := zone.queries.Load(); got != 1 {
		t.Fatalf("%d queries within the TTL, want 1", got)
	}
	now = now.Add(61 * time.Second)
	resolve()
	if got := zone.queries.Load(); got != 2 {
		t.Fatalf("%d queries after the TTL, want 2", got)
	}
}

func TestResolverReturnsACopy(t *testing.T) {
	resolver := newTestResolver(t, Config{}, dualStackZone())
	first, _ := resolver.ResolveDomain(context.Background(), "example.com")
	first[0] = netip.MustParseAddr("203.0.113.66")
	second, _ := resolver.ResolveDomain(context.Background(), "example.com")
	if second[0] != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("a caller's change reached the cache: %v", second)
	}
}

func TestResolverCachesNegativeAnswers(t *testing.T) {
	zone := newFakeZone()
	resolver := newTestResolver(t, Config{}, zone)
	for range 3 {
		_, err := resolver.ResolveDomain(context.Background(), "missing.example")
		if !errors.Is(err, ErrNoAddresses) {
			t.Fatalf("err = %v, want ErrNoAddresses", err)
		}
	}
	if got := zone.queries.Load(); got != 2 {
		t.Fatalf("%d queries, want one per record type", got)
	}
}

func TestAFailedLookupMatchesErrLookupFailedAndKeepsItsText(t *testing.T) {
	failing := dualStackZone()
	failing.failWith = errFakeFailure
	resolver := newTestResolver(t, Config{}, failing)

	_, err := resolver.ResolveDomain(context.Background(), "example.com")
	if !errors.Is(err, ErrLookupFailed) || !errors.Is(err, errFakeFailure) {
		t.Fatalf("err = %v, want both ErrLookupFailed and the cause", err)
	}
	if err.Error() != errFakeFailure.Error() && !strings.Contains(err.Error(), errFakeFailure.Error()) {
		t.Errorf("err text = %q, want the cause's text", err.Error())
	}

	_, err = newTestResolver(t, Config{}, newFakeZone()).ResolveDomain(context.Background(), "missing.example")
	if !errors.Is(err, ErrLookupFailed) || !errors.Is(err, ErrNoAddresses) {
		t.Errorf("an unanswerable name: err = %v, want ErrLookupFailed and ErrNoAddresses", err)
	}

	if _, err := resolver.ResolveDomainWithServer(context.Background(), "example.com", "no-such-server"); errors.Is(err, ErrLookupFailed) {
		t.Errorf("an unknown server tag is misuse, not a failed lookup: %v", err)
	}
}

func TestResolverDoesNotCacheFailures(t *testing.T) {
	zone := dualStackZone()
	zone.failWith = errFakeFailure
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)
	for range 2 {
		if _, err := resolver.ResolveDomain(context.Background(), "example.com"); !errors.Is(err, errFakeFailure) {
			t.Fatalf("err = %v", err)
		}
	}
	if got := zone.queries.Load(); got != 2 {
		t.Fatalf("%d queries, want 2: a failure must not be cached", got)
	}
}

func TestResolverReturnsOneFamilyWhenTheOtherFails(t *testing.T) {
	zone := newFakeZone()
	zone.addresses["v4only.example."] = addresses("192.0.2.7")
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsPreferIPv6}, zone)
	got, err := resolver.ResolveDomain(context.Background(), "v4only.example")
	if err != nil || !slices.Equal(got, addresses("192.0.2.7")) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestConcurrentLookupsShareOneQuery(t *testing.T) {
	zone := dualStackZone()
	zone.release = make(chan struct{})
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)

	var wait sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wait.Go(func() {
			_, err := resolver.ResolveDomain(context.Background(), "example.com")
			errs <- err
		})
	}
	waitFor(t, func() bool { return zone.queries.Load() == 1 })
	time.Sleep(20 * time.Millisecond)
	close(zone.release)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := zone.queries.Load(); got != 1 {
		t.Fatalf("%d queries for 20 concurrent lookups, want 1", got)
	}
}

func TestACancelledLookupDoesNotCancelTheSharedQuery(t *testing.T) {
	zone := dualStackZone()
	zone.release = make(chan struct{})
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only}, zone)

	impatient, cancel := context.WithCancel(context.Background())
	impatientDone := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveDomain(impatient, "example.com")
		impatientDone <- err
	}()
	waitFor(t, func() bool { return zone.queries.Load() == 1 })

	patientDone := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveDomain(context.Background(), "example.com")
		patientDone <- err
	}()
	cancel()
	if err := <-impatientDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("impatient lookup: %v", err)
	}
	close(zone.release)
	if err := <-patientDone; err != nil {
		t.Fatalf("patient lookup: %v", err)
	}
}

func TestDisabledCacheAsksEveryTime(t *testing.T) {
	zone := dualStackZone()
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsIPv4Only, Cache: blueprint.DNSCache{Disable: true}}, zone)
	for range 3 {
		resolver.ResolveDomain(context.Background(), "example.com")
	}
	if got := zone.queries.Load(); got != 3 {
		t.Fatalf("%d queries, want 3", got)
	}
}

func TestCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	cache := newAnswerCache(2, 0)
	now := time.Unix(1000, 0)
	stored := answer{addresses: addresses("192.0.2.1"), ttl: time.Minute}
	first, second, third := cacheKey{name: "a"}, cacheKey{name: "b"}, cacheKey{name: "c"}
	cache.store(first, stored, now)
	cache.store(second, stored, now)
	cache.lookup(first, now)
	cache.store(third, stored, now)

	if _, freshness := cache.lookup(second, now); freshness != cacheMissing {
		t.Fatal("the least recently used entry survived")
	}
	for _, kept := range []cacheKey{first, third} {
		if _, freshness := cache.lookup(kept, now); freshness != cacheFresh {
			t.Fatalf("%+v was evicted", kept)
		}
	}
	if cache.len() != 2 {
		t.Fatalf("%d entries, want 2", cache.len())
	}
}

func TestCacheDoesNotKeepAZeroTTL(t *testing.T) {
	cache := newAnswerCache(4, 0)
	cache.store(cacheKey{name: "a"}, answer{addresses: addresses("192.0.2.1")}, time.Unix(0, 0))
	if cache.len() != 0 {
		t.Fatal("an answer with no TTL was cached")
	}
}

func TestRulesPickTheServer(t *testing.T) {
	domestic, foreign := newFakeZone(), newFakeZone()
	domestic.addresses["shop.ir."] = addresses("192.0.2.10")
	foreign.addresses["shop.ir."] = addresses("203.0.113.10")
	foreign.addresses["example.com."] = addresses("203.0.113.20")

	irDomains := &dataset.Dataset{Kind: blueprint.DatasetKindDomain, Domains: match.NewDomainMatcher(match.DomainList{Suffixes: []string{"ir"}})}
	resolver := NewResolver(Config{IPVersions: blueprint.IPVersionsIPv4Only})
	err := resolver.AttachServers(
		[]Server{{Tag: "domestic", Wire: domestic}, {Tag: "foreign", Wire: foreign}},
		[]blueprint.DNSRule{{Match: blueprint.Match{DomainDatasets: []string{"ir-domains"}}, Server: "domestic"}},
		"foreign",
		map[string]*dataset.Dataset{"ir-domains": irDomains},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resolver.Close()

	for name, want := range map[string]string{"shop.ir": "192.0.2.10", "example.com": "203.0.113.20"} {
		got, err := resolver.ResolveDomain(context.Background(), name)
		if err != nil || !slices.Equal(got, addresses(want)) {
			t.Errorf("%s: got %v, %v; want %s", name, got, err, want)
		}
	}

	got, err := resolver.ResolveDomainWithServer(context.Background(), "shop.ir", "foreign")
	if err != nil || !slices.Equal(got, addresses("203.0.113.10")) {
		t.Fatalf("a lookup naming its server got %v, %v; the rule-chosen answer must not leak into it", got, err)
	}
	pinned, err := resolver.PinnedTo("foreign").ResolveDomain(context.Background(), "shop.ir")
	if err != nil || !slices.Equal(pinned, addresses("203.0.113.10")) {
		t.Fatalf("pinned: got %v, %v", pinned, err)
	}
}

func TestRuleCombinators(t *testing.T) {
	condition, err := compileNameMatch(blueprint.Match{
		DomainSuffixes: []string{"example.com"},
		Not:            &blueprint.Match{Any: []blueprint.Match{{Domains: []string{"ads.example.com"}}, {DomainRegexes: []*regexp.Regexp{regexp.MustCompile(`^track`)}}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"www.example.com": true, "example.com": true, "ads.example.com": false,
		"tracker.example.com": false, "example.org": false,
	} {
		if got := condition.matches(name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}

func TestRulesRefuseNonDomainFieldsAndUnknownNames(t *testing.T) {
	resolver := NewResolver(Config{})
	port := []blueprint.PortRange{{First: 53, Last: 53}}
	err := resolver.AttachServers([]Server{{Tag: "fake", Wire: newFakeZone()}},
		[]blueprint.DNSRule{
			{Match: blueprint.Match{Ports: port}, Server: "fake"},
			{Match: blueprint.Match{Domains: []string{"a"}}, Server: "missing"},
			{Match: blueprint.Match{DomainDatasets: []string{"missing"}}, Server: "fake"},
		}, "absent", nil)
	if err == nil {
		t.Fatal("refused nothing")
	}
	for _, want := range []string{"only on the name", `no dns server "missing"`, `no dataset "missing"`, `no default server "absent"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestAttachServersRefusesBadServersAndASecondAttach(t *testing.T) {
	resolver := NewResolver(Config{})
	if err := resolver.AttachServers([]Server{{Tag: "both", Wire: newFakeZone(), System: NewSystemServer(0)}}, nil, "both", nil); err == nil {
		t.Fatal("a server with two kinds was accepted")
	}
	if err := resolver.AttachServers([]Server{{Tag: "a", Wire: newFakeZone()}, {Tag: "a", Wire: newFakeZone()}}, nil, "a", nil); err == nil {
		t.Fatal("two servers with one tag were accepted")
	}
	if err := resolver.AttachServers([]Server{{Tag: "a", Wire: newFakeZone()}}, nil, "a", nil); err != nil {
		t.Fatal(err)
	}
	if err := resolver.AttachServers([]Server{{Tag: "b", Wire: newFakeZone()}}, nil, "b", nil); !errors.Is(err, errServersAttached) {
		t.Fatalf("second attach: %v", err)
	}
}

func TestResolverBeforeAttachIsNotReady(t *testing.T) {
	resolver := NewResolver(Config{})
	if _, err := resolver.ResolveDomain(context.Background(), "example.com"); !errors.Is(err, errResolverNotReady) {
		t.Fatalf("err = %v", err)
	}
}

func TestLiteralsAndLocalhostNeedNoServer(t *testing.T) {
	zone := newFakeZone()
	resolver := newTestResolver(t, Config{IPVersions: blueprint.IPVersionsPreferIPv4}, zone)
	for domain, want := range map[string][]netip.Addr{
		"192.0.2.5":        addresses("192.0.2.5"),
		"[2001:db8::5]":    addresses("2001:db8::5"),
		"localhost":        addresses("127.0.0.1", "::1"),
		"app.localhost.":   addresses("127.0.0.1", "::1"),
		"::ffff:192.0.2.6": addresses("192.0.2.6"),
	} {
		got, err := resolver.ResolveDomain(context.Background(), domain)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: got %v, %v; want %v", domain, got, err, want)
		}
	}
	if zone.queries.Load() != 0 {
		t.Fatalf("%d queries, want none", zone.queries.Load())
	}
}

func TestResolverRefusesNamesAQueryCannotCarry(t *testing.T) {
	zone := newFakeZone()
	resolver := newTestResolver(t, Config{}, zone)
	if _, err := resolver.ResolveDomain(context.Background(), "evil\r\nname.example"); err == nil {
		t.Fatal("accepted a name with control bytes")
	}
	if zone.queries.Load() != 0 {
		t.Fatal("sent a query for an invalid name")
	}
}

// loopingZone resolves through the resolver itself, as a DNS server reached
// through a proxy whose own name needs that server would.
type loopingZone struct {
	resolver *Resolver
	inner    *fakeZone
}

func (z *loopingZone) Query(ctx context.Context, queryMessage []byte) ([]byte, error) {
	ctx = ContextWithOutboundInLookupPath(ctx, "proxy")
	if _, err := z.resolver.ResolveDomain(ctx, "proxy.example"); err != nil {
		return nil, err
	}
	return z.inner.Query(ctx, queryMessage)
}

func (z *loopingZone) Close() error { return nil }

func TestALookupThatNeedsItselfFailsAndNamesTheLoop(t *testing.T) {
	resolver := NewResolver(Config{IPVersions: blueprint.IPVersionsIPv4Only})
	looping := &loopingZone{resolver: resolver, inner: dualStackZone()}
	if err := resolver.AttachServers([]Server{{Tag: "google", Wire: looping}}, nil, "google", nil); err != nil {
		t.Fatal(err)
	}
	defer resolver.Close()

	done := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveDomain(context.Background(), "example.com")
		done <- err
	}()
	select {
	case err := <-done:
		var loop *LoopError
		if !errors.As(err, &loop) {
			t.Fatalf("err = %v, want a LoopError", err)
		}
		want := `dns server "google" → outbound "proxy" → dns server "google"`
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name the loop %s", err, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the loop hung")
	}
}

func TestCloseStopsQueriesInFlight(t *testing.T) {
	zone := dualStackZone()
	zone.release = make(chan struct{})
	resolver := NewResolver(Config{IPVersions: blueprint.IPVersionsIPv4Only})
	if err := resolver.AttachServers([]Server{{Tag: "fake", Wire: zone}}, nil, "fake", nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := resolver.ResolveDomain(context.Background(), "example.com")
		done <- err
	}()
	waitFor(t, func() bool { return zone.queries.Load() == 1 })

	closed := make(chan struct{})
	go func() { resolver.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited on a query that never ends")
	}
	if err := <-done; err == nil {
		t.Fatal("the lookup succeeded after Close")
	}
	if _, err := resolver.ResolveDomain(context.Background(), "other.example"); !errors.Is(err, errResolverClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestSystemServer(t *testing.T) {
	system := NewSystemServer(0)
	system.lookupNetIP = func(_ context.Context, network, host string) ([]netip.Addr, error) {
		if host == "missing.example" {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		if network == "ip4" {
			return addresses("::ffff:192.0.2.40"), nil
		}
		return addresses("2001:db8::40"), nil
	}
	resolver := NewResolver(Config{IPVersions: blueprint.IPVersionsPreferIPv4})
	if err := resolver.AttachServers([]Server{{Tag: "os", System: system}}, nil, "os", nil); err != nil {
		t.Fatal(err)
	}
	defer resolver.Close()

	got, err := resolver.ResolveDomain(context.Background(), "example.com")
	if err != nil || !slices.Equal(got, addresses("192.0.2.40", "2001:db8::40")) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := resolver.ResolveDomain(context.Background(), "missing.example"); !errors.Is(err, ErrNoAddresses) {
		t.Fatalf("missing: %v", err)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(time.Millisecond)
	}
}
