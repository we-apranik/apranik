package stats

import (
	"sync/atomic"
	"time"
)

// DNSFailureReason says why a lookup produced no address. A fixed list.
type DNSFailureReason uint8

const (
	DNSTimeout DNSFailureReason = iota
	DNSNoAnswer
	DNSTruncated
	DNSServerFailed
	DNSOther
	dnsFailureReasonCount
)

var dnsFailureReasonNames = [dnsFailureReasonCount]string{
	DNSTimeout:      "timeout",
	DNSNoAnswer:     "no_answer",
	DNSTruncated:    "truncated",
	DNSServerFailed: "server_failed",
	DNSOther:        "other",
}

func (r DNSFailureReason) String() string {
	if r >= dnsFailureReasonCount {
		return dnsFailureReasonNames[DNSOther]
	}
	return dnsFailureReasonNames[r]
}

// DNSCounters is the resolver's numbers. The Registry makes them and hands
// them to the resolver, which counts into them; the resolver knows nothing of
// who reads. Methods do nothing on a nil *DNSCounters, which is counting off.
//
// A lookup is one name asked of the resolver; an address literal and
// "localhost" never reach a server and are not counted. A cache hit or a
// stale answer is one record type answered from the cache, so a name looked
// up for both address families can count two. A query is one exchange with a
// server, which a cache hit never makes.
type DNSCounters struct {
	lookups       atomic.Uint64
	cacheHits     atomic.Uint64
	staleAnswers  atomic.Uint64
	lookupFailure [dnsFailureReasonCount]atomic.Uint64

	// One entry per DNS server tag, made once so reads take no lock.
	servers map[string]*DNSServerCounters
}

// DNSServerCounters is one DNS server's numbers.
type DNSServerCounters struct {
	queries           atomic.Uint64
	failures          atomic.Uint64
	totalLatencyNanos atomic.Int64
}

func NewDNSCounters(serverTags []string) *DNSCounters {
	counters := &DNSCounters{servers: make(map[string]*DNSServerCounters, len(serverTags))}
	for _, tag := range serverTags {
		counters.servers[tag] = &DNSServerCounters{}
	}
	return counters
}

func (c *DNSCounters) LookupStarted() {
	if c != nil {
		c.lookups.Add(1)
	}
}

func (c *DNSCounters) CacheHit() {
	if c != nil {
		c.cacheHits.Add(1)
	}
}

// A stale answer was served while a fresh one is fetched.
func (c *DNSCounters) StaleAnswerServed() {
	if c != nil {
		c.staleAnswers.Add(1)
	}
}

// A lookup ended with no address. The reason is one of the fixed list; one
// outside it counts as DNSOther.
func (c *DNSCounters) LookupFailed(reason DNSFailureReason) {
	if c == nil {
		return
	}
	if reason >= dnsFailureReasonCount {
		reason = DNSOther
	}
	c.lookupFailure[reason].Add(1)
}

// Nil for a nil receiver or a tag the counters were not made for.
func (c *DNSCounters) Server(tag string) *DNSServerCounters {
	if c == nil {
		return nil
	}
	return c.servers[tag]
}

// One exchange with the server ended, in took, with failed saying whether it
// produced an answer.
func (s *DNSServerCounters) QueryFinished(took time.Duration, failed bool) {
	if s == nil {
		return
	}
	s.queries.Add(1)
	s.totalLatencyNanos.Add(int64(took))
	if failed {
		s.failures.Add(1)
	}
}

// DNSTraffic is a copy of the resolver's numbers. Failures is the sum of
// FailuresByReason, which holds only the reasons that happened, by name.
type DNSTraffic struct {
	Lookups          uint64
	CacheHits        uint64
	StaleAnswers     uint64
	Failures         uint64
	FailuresByReason map[string]uint64
	Servers          map[string]DNSServerTraffic
}

// DNSServerTraffic is a copy of one server's numbers; the mean latency is
// TotalLatency over Queries.
type DNSServerTraffic struct {
	Queries      uint64
	Failures     uint64
	TotalLatency time.Duration
}

func (c *DNSCounters) snapshot() DNSTraffic {
	if c == nil {
		return DNSTraffic{}
	}
	var failures [dnsFailureReasonCount]uint64
	var failed uint64
	for reason := range c.lookupFailure {
		failures[reason] = c.lookupFailure[reason].Load()
		failed += failures[reason]
	}
	servers := make(map[string]DNSServerTraffic, len(c.servers))
	for tag, server := range c.servers {
		servers[tag] = DNSServerTraffic{
			Queries:      server.queries.Load(),
			Failures:     server.failures.Load(),
			TotalLatency: time.Duration(server.totalLatencyNanos.Load()),
		}
	}
	return DNSTraffic{
		Lookups:          c.lookups.Load(),
		CacheHits:        c.cacheHits.Load(),
		StaleAnswers:     c.staleAnswers.Load(),
		Failures:         failed,
		FailuresByReason: nonzeroByName(dnsFailureReasonNames[:], failures[:]),
		Servers:          servers,
	}
}
