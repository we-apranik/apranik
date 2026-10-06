package stats

import (
	"maps"
	"testing"
	"time"
)

func TestDNSCountersReportEveryNumberAndSumTheFailures(t *testing.T) {
	registry := NewRegistry(Config{DNSServerTags: []string{"cloudflare", "system"}})
	counters := registry.DNSCounters()
	for range 5 {
		counters.LookupStarted()
	}
	counters.CacheHit()
	counters.CacheHit()
	counters.StaleAnswerServed()
	counters.LookupFailed(DNSTimeout)
	counters.LookupFailed(DNSNoAnswer)
	counters.LookupFailed(DNSNoAnswer)
	counters.LookupFailed(DNSFailureReason(200)) // outside the list
	counters.Server("cloudflare").QueryFinished(30*time.Millisecond, false)
	counters.Server("cloudflare").QueryFinished(50*time.Millisecond, true)

	got := registry.DNS()
	if got.Lookups != 5 || got.CacheHits != 2 || got.StaleAnswers != 1 || got.Failures != 4 {
		t.Errorf("dns = %+v", got)
	}
	if want := map[string]uint64{"timeout": 1, "no_answer": 2, "other": 1}; !maps.Equal(got.FailuresByReason, want) {
		t.Errorf("by reason = %v, want %v", got.FailuresByReason, want)
	}
	if server := got.Servers["cloudflare"]; server.Queries != 2 || server.Failures != 1 || server.TotalLatency != 80*time.Millisecond {
		t.Errorf("cloudflare = %+v", server)
	}
	if server, listed := got.Servers["system"]; !listed || server.Queries != 0 {
		t.Errorf("system = %+v, listed %v: a server with no queries is still listed", server, listed)
	}
}

func TestDNSCountersOnANilRegistryOrNilCountersDoNothing(t *testing.T) {
	var registry *Registry
	if registry.DNSCounters() != nil || registry.DNS().Lookups != 0 {
		t.Error("a nil registry has DNS counters")
	}
	var counters *DNSCounters
	counters.LookupStarted()
	counters.CacheHit()
	counters.StaleAnswerServed()
	counters.LookupFailed(DNSTimeout)
	counters.Server("any").QueryFinished(time.Second, true)
	var server *DNSServerCounters
	server.QueryFinished(time.Second, false)
}

func TestAnUnknownDNSServerTagHasNoCounters(t *testing.T) {
	counters := NewDNSCounters([]string{"system"})
	if counters.Server("system") == nil || counters.Server("other") != nil {
		t.Error("only a tag the counters were made for has a server entry")
	}
}

func TestEveryDNSFailureReasonHasADistinctName(t *testing.T) {
	seen := map[string]bool{}
	for reason := DNSFailureReason(0); reason < dnsFailureReasonCount; reason++ {
		if reason.String() == "" || seen[reason.String()] {
			t.Errorf("dns failure reason %d has no distinct name: %q", reason, reason.String())
		}
		seen[reason.String()] = true
	}
}

func BenchmarkDNSCacheHit(b *testing.B) {
	counters := NewDNSCounters([]string{"system"})
	b.ReportAllocs()
	for b.Loop() {
		counters.CacheHit()
	}
}
