package stats

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type OutboundDescription struct {
	Tag     string
	IsGroup bool
}

// Config says what exists and what to count. An empty tracked list means
// everything of that kind; a list narrows counting to the names in it.
type Config struct {
	InboundTags []string
	Outbounds   []OutboundDescription

	// Where each routing rule sits in the config, plus the default, as
	// router.RuleLocation and router.DefaultRuleLocation write them. Hits are
	// counted per location; the set is fixed here and never grows.
	RuleLocations []string

	// The tags of the DNS servers, so the resolver's counters are made once and
	// never change. Include the operating system's server, tagged "system",
	// which a config with no dns section uses.
	DNSServerTags []string

	// Whether live conversations are listed: each TCP connection, and each UDP
	// destination of a session. Each one costs a small entry for as long as it lives, which is why it is a setting: the builder turns it
	// on when something reads it, such as an API.
	TrackLiveConversations bool

	// Whether each outbound's dials are timed (dial health). The handler reads
	// the clock around a dial only when this is on.
	TimeDials bool

	TrackedInboundTags  []string
	TrackedOutboundTags []string
	TrackedUsers        []string
}

// Registry holds every key's Counters. Inbound and outbound counters are made
// once, so looking them up takes no lock. A user's counters are made the first
// time core asks for that user, which core does only after the protocol has
// authenticated the user, so a peer that never authenticates creates nothing.
//
// A nil *Registry is counting switched off: every lookup returns nil, and
// callers skip nil counters.
type Registry struct {
	startedAt time.Time

	inbounds map[string]*InboundCounters
	// The same counters as inbounds, in a slice, because summing a map costs
	// several times what summing a slice does and TotalTraffic is read often.
	inboundList []*InboundCounters
	outbounds   map[string]*outboundCounters

	// One counter per rule location, made once so reads take no lock.
	ruleHits map[string]*atomic.Uint64

	dns                    *DNSCounters
	timeDials              bool
	trackLiveConversations bool
	lastConversationID     atomic.Uint64
	liveShards             [liveShardCount]liveShard

	trackedUsers []string

	usersLock sync.Mutex
	users     map[string]*Counters
}

// The live conversations are split by number over several locks, so connections
// that begin and end at the same moment, as UDP destinations do, rarely wait
// for each other.
const liveShardCount = 16

type liveShard struct {
	mu            sync.Mutex
	conversations map[uint64]*Conversation

	// Keeps two shards' locks off one cache line.
	_ [48]byte
}

type outboundCounters struct {
	counters OutboundCounters
	isGroup  bool
}

func NewRegistry(config Config) *Registry {
	registry := &Registry{
		startedAt:              time.Now(),
		inbounds:               make(map[string]*InboundCounters, len(config.InboundTags)),
		outbounds:              make(map[string]*outboundCounters, len(config.Outbounds)),
		ruleHits:               make(map[string]*atomic.Uint64, len(config.RuleLocations)),
		dns:                    NewDNSCounters(config.DNSServerTags),
		timeDials:              config.TimeDials,
		trackLiveConversations: config.TrackLiveConversations,
		trackedUsers:           slices.Clone(config.TrackedUsers),
		users:                  make(map[string]*Counters),
	}
	for _, location := range config.RuleLocations {
		registry.ruleHits[location] = &atomic.Uint64{}
	}
	for _, tag := range config.InboundTags {
		if isTracked(config.TrackedInboundTags, tag) {
			counters := &InboundCounters{}
			registry.inbounds[tag] = counters
			registry.inboundList = append(registry.inboundList, counters)
		}
	}
	for _, outbound := range config.Outbounds {
		if isTracked(config.TrackedOutboundTags, outbound.Tag) {
			registry.outbounds[outbound.Tag] = &outboundCounters{isGroup: outbound.IsGroup}
		}
	}
	return registry
}

func isTracked(trackedNames []string, name string) bool {
	return len(trackedNames) == 0 || slices.Contains(trackedNames, name)
}

// Nil when counting is off, the tag is unknown or the tag is not tracked.
func (r *Registry) Inbound(tag string) *InboundCounters {
	if r == nil {
		return nil
	}
	return r.inbounds[tag]
}

// Nil when counting is off, the tag is unknown or the tag is not tracked.
func (r *Registry) Outbound(tag string) *OutboundCounters {
	if r == nil {
		return nil
	}
	if outbound, found := r.outbounds[tag]; found {
		return &outbound.counters
	}
	return nil
}

// Nil when counting is off, the name is empty or the user is not tracked. The
// caller asks once per connection and keeps the answer.
func (r *Registry) User(name string) *Counters {
	if r == nil || name == "" || !isTracked(r.trackedUsers, name) {
		return nil
	}
	r.usersLock.Lock()
	defer r.usersLock.Unlock()
	counters, found := r.users[name]
	if !found {
		counters = &Counters{}
		r.users[name] = counters
	}
	return counters
}

// When this process began counting. Counters never reset, so a reader that
// sees it change knows they began again from zero.
func (r *Registry) StartedAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.startedAt
}

// A copy that never shares memory with the live counters. Counters are read
// one at a time, so a copy taken during traffic is not one instant: bytes and
// connection counts can differ by what moved between two reads.
func (r *Registry) InboundTraffic() map[string]InboundTraffic {
	if r == nil {
		return nil
	}
	traffic := make(map[string]InboundTraffic, len(r.inbounds))
	for tag, counters := range r.inbounds {
		traffic[tag] = counters.Snapshot()
	}
	return traffic
}

// A copy, with groups marked, so a reader summing across outbounds knows to
// skip them.
func (r *Registry) OutboundTraffic() map[string]OutboundTraffic {
	if r == nil {
		return nil
	}
	traffic := make(map[string]OutboundTraffic, len(r.outbounds))
	for tag, outbound := range r.outbounds {
		outboundTraffic := outbound.counters.Snapshot()
		outboundTraffic.IsGroup = outbound.isGroup
		traffic[tag] = outboundTraffic
	}
	return traffic
}

// A copy that grows with the users core has authenticated, so a reader that
// only wants totals asks TotalTraffic instead.
func (r *Registry) UserTraffic() map[string]Traffic {
	if r == nil {
		return nil
	}
	r.usersLock.Lock()
	defer r.usersLock.Unlock()
	traffic := make(map[string]Traffic, len(r.users))
	for name, counters := range r.users {
		traffic[name] = counters.Snapshot()
	}
	return traffic
}

// The payload every counted inbound carried, by network and together. Each
// connection and each UDP session enters through exactly one inbound, so unlike
// the outbounds this never counts a group's traffic twice. It allocates
// nothing, so a reader may ask every second.
func (r *Registry) TotalTraffic() TrafficTotals {
	if r == nil {
		return TrafficTotals{}
	}
	var tcpUplink, tcpDownlink, udpUplink, udpDownlink uint64
	for _, counters := range r.inboundList {
		base := &counters.base
		tcpUplink += base.tcpUplinkBytes.Load()
		tcpDownlink += base.tcpDownlinkBytes.Load()
		udpUplink += base.udpUplinkBytes.Load()
		udpDownlink += base.udpDownlinkBytes.Load()
	}
	totals := TrafficTotals{
		All: NetworkBytes{UplinkBytes: tcpUplink + udpUplink, DownlinkBytes: tcpDownlink + udpDownlink},
		TCP: NetworkBytes{UplinkBytes: tcpUplink, DownlinkBytes: tcpDownlink},
		UDP: NetworkBytes{UplinkBytes: udpUplink, DownlinkBytes: udpDownlink},
	}
	return totals
}

// A routing rule decided a connection. The location is a Decision's Rule; one
// the registry was not told about is ignored, so a peer can never grow the set.
func (r *Registry) RuleHit(location string) {
	if r == nil {
		return
	}
	if hits, found := r.ruleHits[location]; found {
		hits.Add(1)
	}
}

// Hits per rule location, the rules that never matched included, so a reader
// sees which rules do nothing.
func (r *Registry) RuleHits() map[string]uint64 {
	if r == nil {
		return nil
	}
	hits := make(map[string]uint64, len(r.ruleHits))
	for location, count := range r.ruleHits {
		hits[location] = count.Load()
	}
	return hits
}

// Whether the handler should time each dial. A nil registry times nothing.
func (r *Registry) TimesDials() bool {
	return r != nil && r.timeDials
}

// What the resolver counts into: nil when counting is off, which the resolver
// reads as counting nothing.
func (r *Registry) DNSCounters() *DNSCounters {
	if r == nil {
		return nil
	}
	return r.dns
}

// A copy of the resolver's numbers. The zero DNSTraffic for a nil registry.
func (r *Registry) DNS() DNSTraffic {
	if r == nil {
		return DNSTraffic{}
	}
	return r.dns.snapshot()
}
