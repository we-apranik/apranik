package stats

import (
	"fmt"
	"maps"
	"sync"
	"testing"
)

func newTestRegistry(config Config) *Registry {
	if config.InboundTags == nil {
		config.InboundTags = []string{"socks-in", "http-in"}
	}
	if config.Outbounds == nil {
		config.Outbounds = []OutboundDescription{{Tag: "server-a"}, {Tag: "server-b"}, {Tag: "auto", IsGroup: true}}
	}
	return NewRegistry(config)
}

func TestEveryInboundAndOutboundIsCountedByDefault(t *testing.T) {
	registry := newTestRegistry(Config{})

	for _, tag := range []string{"socks-in", "http-in"} {
		if registry.Inbound(tag) == nil {
			t.Errorf("inbound %q has no counters", tag)
		}
	}
	for _, tag := range []string{"server-a", "server-b", "auto"} {
		if registry.Outbound(tag) == nil {
			t.Errorf("outbound %q has no counters", tag)
		}
	}
	if registry.User("alice") == nil {
		t.Error("user alice has no counters")
	}
}

func TestUnknownTagsAndEmptyUserHaveNoCounters(t *testing.T) {
	registry := newTestRegistry(Config{})

	if registry.Inbound("missing") != nil || registry.Outbound("missing") != nil {
		t.Error("an unknown tag got counters")
	}
	if registry.User("") != nil {
		t.Error("an empty user name got counters")
	}
}

func TestTrackedListsNarrowCounting(t *testing.T) {
	registry := newTestRegistry(Config{
		TrackedInboundTags:  []string{"socks-in"},
		TrackedOutboundTags: []string{"server-a"},
	})

	if registry.Inbound("socks-in") == nil || registry.Inbound("http-in") != nil {
		t.Error("inbound narrowing is wrong")
	}
	if registry.Outbound("server-a") == nil || registry.Outbound("server-b") != nil {
		t.Error("outbound narrowing is wrong")
	}

	if len(registry.InboundTraffic()) != 1 || len(registry.OutboundTraffic()) != 1 {
		t.Errorf("a read holds untracked keys: %+v %+v", registry.InboundTraffic(), registry.OutboundTraffic())
	}
}

func TestSameUserGetsSameCounters(t *testing.T) {
	registry := newTestRegistry(Config{})
	if registry.User("alice") != registry.User("alice") {
		t.Error("two lookups of one user returned different counters")
	}
}

func TestNilRegistryMeansCountingIsOff(t *testing.T) {
	var registry *Registry

	if registry.Inbound("socks-in") != nil || registry.Outbound("auto") != nil || registry.User("alice") != nil {
		t.Error("a nil registry returned counters")
	}
	if !registry.StartedAt().IsZero() || registry.InboundTraffic() != nil || registry.OutboundTraffic() != nil || registry.UserTraffic() != nil {
		t.Error("a nil registry returned a non-zero read")
	}
	if totals := registry.TotalTraffic(); totals != (TrafficTotals{}) {
		t.Errorf("a nil registry returned totals %+v", totals)
	}
}

func TestReadsReportCountersAndMarkGroups(t *testing.T) {
	registry := newTestRegistry(Config{})

	inbound := registry.Inbound("socks-in")
	inbound.Base().ConnectionStarted()
	inbound.Base().AddUplinkBytes(100)
	inbound.Base().AddDownlinkBytes(2000)
	for range 3 {
		inbound.ConnectionRefusedFor(RefusalMaxTCPConnections)
	}
	inbound.ConnectionSentToFallback()
	registry.Outbound("server-a").DialFailedFor(DialRefused)
	registry.User("alice").AddDownlinkBytes(7)

	got := registry.InboundTraffic()["socks-in"]
	wantTraffic := Traffic{UplinkBytes: 100, DownlinkBytes: 2000, TCP: TCPTraffic{UplinkBytes: 100, DownlinkBytes: 2000, ActiveConnections: 1, TotalConnections: 1}}
	if got.Traffic != wantTraffic ||
		got.RefusedConnections != 3 || got.FallbackConnections != 1 {
		t.Errorf("inbound traffic = %+v", got)
	}
	outbounds := registry.OutboundTraffic()
	if got := outbounds["server-a"]; got.DialFailures != 1 || got.IsGroup {
		t.Errorf("server-a = %+v", got)
	}
	if !outbounds["auto"].IsGroup {
		t.Error("auto is not marked as a group")
	}
	if alice := registry.UserTraffic()["alice"]; alice.DownlinkBytes != 7 {
		t.Errorf("alice = %+v", alice)
	}
	if registry.StartedAt().IsZero() {
		t.Error("StartedAt is zero")
	}
}

func TestAReadIsACopy(t *testing.T) {
	registry := newTestRegistry(Config{})
	counters := registry.Inbound("socks-in").Base()
	counters.AddUplinkBytes(10)

	copied := registry.InboundTraffic()
	counters.AddUplinkBytes(10)
	copied["socks-in"] = InboundTraffic{}

	if got := registry.InboundTraffic()["socks-in"].UplinkBytes; got != 20 {
		t.Errorf("uplink bytes after taking and changing a copy = %d, want 20", got)
	}
}

func TestTotalTrafficSumsTheCountedInboundsOnly(t *testing.T) {
	registry := newTestRegistry(Config{TrackedInboundTags: []string{"socks-in"}})
	registry.Inbound("socks-in").Base().AddUplinkBytes(30)
	registry.Inbound("socks-in").Base().AddDownlinkBytes(70)
	registry.Inbound("http-in").Base().AddUplinkBytes(1000)
	registry.Outbound("server-a").Base().AddUplinkBytes(500)

	totals := registry.TotalTraffic()
	want := NetworkBytes{UplinkBytes: 30, DownlinkBytes: 70}
	if totals.All != want || totals.TCP != want || totals.UDP != (NetworkBytes{}) {
		t.Errorf("totals = %+v, want %+v for all and TCP, none for UDP: only counted inbounds, never outbounds", totals, want)
	}
}

func TestConnectionEndedBringsActiveBackDown(t *testing.T) {
	var counters Counters
	counters.ConnectionStarted()
	counters.ConnectionStarted()
	counters.ConnectionEnded()

	traffic := counters.Snapshot()
	if traffic.TCP.ActiveConnections != 1 || traffic.TCP.TotalConnections != 2 {
		t.Errorf("traffic = %+v, want 1 active of 2 total", traffic)
	}
}

func TestConcurrentCountingAndReading(t *testing.T) {
	registry := newTestRegistry(Config{})
	const connections, readsPerConnection, bytesPerRead = 50, 200, 10

	var connectionsRunning sync.WaitGroup
	for range connections {
		connectionsRunning.Add(1)
		go func() {
			defer connectionsRunning.Done()
			inbound, outbound, user := registry.Inbound("socks-in").Base(), registry.Outbound("server-a").Base(), registry.User("alice")
			inbound.ConnectionStarted()
			defer inbound.ConnectionEnded()
			for range readsPerConnection {
				inbound.AddUplinkBytes(bytesPerRead)
				outbound.AddUplinkBytes(bytesPerRead)
				user.AddUplinkBytes(bytesPerRead)
				_ = registry.InboundTraffic()
				_ = registry.TotalTraffic()
			}
		}()
	}
	connectionsRunning.Wait()

	want := uint64(connections * readsPerConnection * bytesPerRead)
	inbound := registry.InboundTraffic()["socks-in"]
	outbound := registry.OutboundTraffic()["server-a"]
	user := registry.UserTraffic()["alice"]
	if inbound.UplinkBytes != want || outbound.UplinkBytes != want || user.UplinkBytes != want {
		t.Errorf("uplink bytes = %d / %d / %d, want %d each", inbound.UplinkBytes, outbound.UplinkBytes, user.UplinkBytes, want)
	}
	if inbound.TCP.ActiveConnections != 0 {
		t.Errorf("active connections = %d, want 0", inbound.TCP.ActiveConnections)
	}
	if up := registry.TotalTraffic().All.UplinkBytes; up != want {
		t.Errorf("total uplink = %d, want %d", up, want)
	}
}

func BenchmarkCountOneRead(b *testing.B) {
	registry := newTestRegistry(Config{})
	counters := []*Counters{registry.Inbound("socks-in").Base(), registry.Outbound("server-a").Base(), registry.User("alice")}
	b.ReportAllocs()
	for b.Loop() {
		for _, c := range counters {
			c.AddUplinkBytes(4096)
		}
	}
}

func BenchmarkCountOneReadSharedCountersParallel(b *testing.B) {
	registry := newTestRegistry(Config{})
	counters := []*Counters{registry.Inbound("socks-in").Base(), registry.Outbound("server-a").Base(), registry.User("alice")}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for _, c := range counters {
				c.AddUplinkBytes(4096)
			}
		}
	})
}

func BenchmarkCountOneReadLocalThenFlushParallel(b *testing.B) {
	registry := newTestRegistry(Config{})
	counters := []*Counters{registry.Inbound("socks-in").Base(), registry.Outbound("server-a").Base(), registry.User("alice")}
	const flushEvery = 64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var local uint64
		reads := 0
		for pb.Next() {
			local += 4096
			reads++
			if reads == flushEvery {
				for _, c := range counters {
					c.AddUplinkBytes(local)
				}
				local, reads = 0, 0
			}
		}
	})
}

func BenchmarkTotalTraffic(b *testing.B) {
	registry := newTestRegistry(Config{})
	b.ReportAllocs()
	for b.Loop() {
		_ = registry.TotalTraffic()
	}
}

func BenchmarkInboundTraffic(b *testing.B) {
	registry := newTestRegistry(Config{})
	b.ReportAllocs()
	for b.Loop() {
		_ = registry.InboundTraffic()
	}
}

func BenchmarkUserTraffic(b *testing.B) {
	registry := newTestRegistry(Config{})
	for i := range 1000 {
		registry.User(fmt.Sprintf("user-%d", i))
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = registry.UserTraffic()
	}
}

func TestRefusalsAreCountedByReasonAndTheTotalIsTheirSum(t *testing.T) {
	registry := newTestRegistry(Config{})
	inbound := registry.Inbound("socks-in")
	inbound.ConnectionRefusedFor(RefusalMaxTCPConnections)
	inbound.ConnectionRefusedFor(RefusalMaxTCPConnections)
	inbound.ConnectionRefusedFor(RefusalHandshakeTimedOut)
	inbound.ConnectionRefusedFor(RefusalBlockedByRule)
	inbound.ConnectionRefusedFor(RefusalReason(200)) // outside the list

	got := registry.InboundTraffic()["socks-in"]
	want := map[string]uint64{"max_tcp_connections": 2, "handshake_timed_out": 1, "blocked_by_rule": 1, "other": 1}
	if !maps.Equal(got.RefusalsByReason, want) {
		t.Errorf("by reason = %v, want %v", got.RefusalsByReason, want)
	}
	if got.RefusedConnections != 5 {
		t.Errorf("refused = %d, want the sum 5", got.RefusedConnections)
	}
	if registry.InboundTraffic()["http-in"].RefusalsByReason != nil {
		t.Error("an inbound that refused nothing has a reasons map")
	}
}

func TestAuthenticationFailuresAreCountedApartFromRefusals(t *testing.T) {
	registry := newTestRegistry(Config{})
	inbound := registry.Inbound("socks-in")
	inbound.AuthenticationFailed()
	inbound.AuthenticationFailed()
	inbound.ConnectionSentToFallback()
	inbound.ConnectionSentToFallback()

	got := registry.InboundTraffic()["socks-in"]
	if got.AuthenticationFailures != 2 || got.FallbackConnections != 2 || got.RefusedConnections != 0 {
		t.Errorf("got %+v, want 2 authentication failures and 2 fallbacks, none refused", got)
	}
}

func TestDialFailuresAreCountedByReasonAndTheTotalIsTheirSum(t *testing.T) {
	registry := newTestRegistry(Config{})
	outbound := registry.Outbound("server-a")
	outbound.DialFailedFor(DialTimeout)
	outbound.DialFailedFor(DialDNSFailed)
	outbound.DialFailedFor(DialDNSFailed)
	outbound.DialFailedFor(DialFailureReason(200)) // outside the list

	got := registry.OutboundTraffic()["server-a"]
	want := map[string]uint64{"timeout": 1, "dns_failed": 2, "other": 1}
	if !maps.Equal(got.DialFailuresByReason, want) || got.DialFailures != 4 {
		t.Errorf("got %v (total %d), want %v (total 4)", got.DialFailuresByReason, got.DialFailures, want)
	}
}

func TestEveryReasonHasADistinctName(t *testing.T) {
	seen := map[string]bool{}
	for reason := RefusalReason(0); reason < refusalReasonCount; reason++ {
		if reason.String() == "" || seen["refusal "+reason.String()] {
			t.Errorf("refusal reason %d has no distinct name: %q", reason, reason.String())
		}
		seen["refusal "+reason.String()] = true
	}
	for reason := DialFailureReason(0); reason < dialFailureReasonCount; reason++ {
		if reason.String() == "" || seen["dial "+reason.String()] {
			t.Errorf("dial failure reason %d has no distinct name: %q", reason, reason.String())
		}
		seen["dial "+reason.String()] = true
	}
}

func TestACounterOnANilInboundOrOutboundDoesNothing(t *testing.T) {
	var inbound *InboundCounters
	var outbound *OutboundCounters
	inbound.ConnectionRefusedFor(RefusalMaxTCPConnections)
	inbound.AuthenticationFailed()
	inbound.ConnectionSentToFallback()
	outbound.DialFailedFor(DialTimeout)
	if inbound.Base() != nil || outbound.Base() != nil {
		t.Error("a nil counter returned a base")
	}
	if got := inbound.Snapshot(); got.RefusedConnections != 0 || got.RefusalsByReason != nil {
		t.Errorf("nil inbound snapshot = %+v", got)
	}
}

func BenchmarkRefusedForAReason(b *testing.B) {
	registry := newTestRegistry(Config{})
	inbound := registry.Inbound("socks-in")
	b.ReportAllocs()
	for b.Loop() {
		inbound.ConnectionRefusedFor(RefusalProxyHandshakeFailed)
	}
}

func TestRuleHitsCountPerLocationAndIgnoreOnesTheRegistryWasNotToldAbout(t *testing.T) {
	registry := newTestRegistry(Config{RuleLocations: []string{"routing.rules[0]", "routing.rules[1]", "default"}})
	registry.RuleHit("routing.rules[1]")
	registry.RuleHit("routing.rules[1]")
	registry.RuleHit("default")
	registry.RuleHit("routing.rules[7]") // not a rule this config has

	want := map[string]uint64{"routing.rules[0]": 0, "routing.rules[1]": 2, "default": 1}
	if got := registry.RuleHits(); !maps.Equal(got, want) {
		t.Errorf("hits = %v, want %v: every rule is listed, the unknown one is not", got, want)
	}
}

func TestRuleHitsOnANilRegistryDoNothing(t *testing.T) {
	var registry *Registry
	registry.RuleHit("default")
	if registry.RuleHits() != nil {
		t.Error("a nil registry returned rule hits")
	}
}

func BenchmarkRuleHit(b *testing.B) {
	registry := newTestRegistry(Config{RuleLocations: []string{"routing.rules[0]", "default"}})
	b.ReportAllocs()
	for b.Loop() {
		registry.RuleHit("routing.rules[0]")
	}
}
