package stats

import (
	"net/netip"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/spec"
)

func trackingRegistry() *Registry {
	return newTestRegistry(Config{TrackLiveConversations: true})
}

func sourceEndpoint(t testing.TB, address string) spec.Endpoint {
	t.Helper()
	endpoint, err := spec.NewEndpoint(netip.MustParseAddr(address), 50000)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func destination(t testing.TB, domain string) spec.Destination {
	t.Helper()
	parsed, err := spec.NewDestinationFromDomain(domain, 443)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestConnectionsAreNotTrackedUnlessSwitchedOn(t *testing.T) {
	registry := newTestRegistry(Config{})

	conversation := registry.TrackConversation(ConversationInfo{InboundTag: "socks-in"}, nil)
	if conversation != nil || registry.IsLiveTrackingEnabled() {
		t.Fatalf("tracking is off, yet TrackConversation returned %v", conversation)
	}
	conversation.End()
	if conversation.Bytes() != nil {
		t.Error("an untracked conversation has counters")
	}
	if registry.LiveConversations() != nil || registry.OnlineUsers() != nil {
		t.Error("an untracked registry lists conversations")
	}

	var nilRegistry *Registry
	if nilRegistry.TrackConversation(ConversationInfo{}, nil) != nil || nilRegistry.IsLiveTrackingEnabled() || nilRegistry.LiveConversations() != nil || nilRegistry.OnlineUsers() != nil {
		t.Error("a nil registry tracks conversations")
	}
}

func TestALiveConversationIsListedWithItsOwnBytesUntilItEnds(t *testing.T) {
	registry := trackingRegistry()
	conversation := registry.TrackConversation(ConversationInfo{
		Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice",
		Source: sourceEndpoint(t, "203.0.113.7"), Destination: destination(t, "example.com"),
		OutboundTags: []string{"auto", "server-a"}, Rule: "routing.rules[3]",
	}, nil)
	conversation.Bytes().AddUplinkBytes(40)
	conversation.Bytes().AddDownlinkBytes(900)

	live := registry.LiveConversations()
	if len(live) != 1 {
		t.Fatalf("%d live conversations, want 1", len(live))
	}
	got := live[0]
	if got.Info.InboundTag != "socks-in" || got.Info.User != "alice" || got.Info.Rule != "routing.rules[3]" ||
		got.Info.Source.Addr().String() != "203.0.113.7" || len(got.Info.OutboundTags) != 2 {
		t.Errorf("info = %+v", got.Info)
	}
	if got.UplinkBytes != 40 || got.DownlinkBytes != 900 {
		t.Errorf("bytes = %d up and %d down, want 40 and 900", got.UplinkBytes, got.DownlinkBytes)
	}
	if time.Since(got.StartedAt) > time.Minute || got.StartedAt.IsZero() {
		t.Errorf("StartedAt = %v", got.StartedAt)
	}

	conversation.End()
	if live := registry.LiveConversations(); len(live) != 0 {
		t.Errorf("%d live conversations after End, want 0", len(live))
	}
}

func TestConnectionIdentifiersAreASequenceAndNeverRepeat(t *testing.T) {
	registry := trackingRegistry()
	for range 1000 {
		registry.TrackConversation(ConversationInfo{}, nil)
	}
	registry.TrackConversation(ConversationInfo{}, nil).End()

	live := registry.LiveConversations()
	if len(live) != 1000 {
		t.Fatalf("%d live conversations, want 1000", len(live))
	}
	for index, conversation := range live {
		if want := strconv.Itoa(index + 1); conversation.ID != want {
			t.Fatalf("conversation %d has id %q, want %q: oldest first, numbered in order", index, conversation.ID, want)
		}
	}
	if next := registry.TrackConversation(ConversationInfo{}, nil); next.id != 1002 {
		t.Errorf("next id = %d, want 1002: an ended conversation's number is not reused", next.id)
	}
}

func TestLiveConversationsAreListedOldestFirst(t *testing.T) {
	registry := trackingRegistry()
	for _, tag := range []string{"first", "second", "third"} {
		registry.TrackConversation(ConversationInfo{InboundTag: tag}, nil)
		time.Sleep(2 * time.Millisecond)
	}

	live := registry.LiveConversations()
	if len(live) != 3 || live[0].Info.InboundTag != "first" || live[1].Info.InboundTag != "second" || live[2].Info.InboundTag != "third" {
		t.Errorf("order = %+v", live)
	}
}

func TestAListOfLiveConversationsIsACopy(t *testing.T) {
	registry := trackingRegistry()
	conversation := registry.TrackConversation(ConversationInfo{OutboundTags: []string{"auto", "server-a"}}, nil)
	conversation.Bytes().AddUplinkBytes(1)

	live := registry.LiveConversations()
	live[0].Info.OutboundTags[0] = "changed"
	live[0].UplinkBytes = 999
	conversation.Bytes().AddUplinkBytes(1)

	again := registry.LiveConversations()
	if again[0].Info.OutboundTags[0] != "auto" || again[0].UplinkBytes != 2 {
		t.Errorf("a change to a copy reached the live conversation: %+v", again[0])
	}
}

func TestAnEndedConnectionLeavesNothingBehind(t *testing.T) {
	registry := trackingRegistry()
	for range 500 {
		registry.TrackConversation(ConversationInfo{}, nil).End()
	}
	left := 0
	for index := range registry.liveShards {
		shard := &registry.liveShards[index]
		shard.mu.Lock()
		left += len(shard.conversations)
		shard.mu.Unlock()
	}
	if left != 0 {
		t.Errorf("%d entries left after every conversation ended", left)
	}
}

func TestOnlineUsersGroupLiveConversationsByUserAndSourceAddress(t *testing.T) {
	registry := trackingRegistry()
	track := func(user, address string) {
		registry.TrackConversation(ConversationInfo{User: user, Source: sourceEndpoint(t, address)}, nil)
		time.Sleep(2 * time.Millisecond)
	}
	track("alice", "203.0.113.7")
	track("bob", "198.51.100.2")
	track("alice", "203.0.113.7") // the same address again: still one IP, but the newer time
	track("alice", "203.0.113.9")
	track("", "192.0.2.1") // no authentication, so no user

	users := registry.OnlineUsers()
	if len(users) != 2 || users[0].Name != "alice" || users[1].Name != "bob" {
		t.Fatalf("users = %+v, want alice and bob only, by name", users)
	}
	alice := users[0]
	if alice.Conversations != 3 || len(alice.IPs) != 2 {
		t.Fatalf("alice = %+v, want 3 conversations from 2 addresses", alice)
	}
	if alice.IPs[0].Address.String() != "203.0.113.7" || alice.IPs[1].Address.String() != "203.0.113.9" {
		t.Errorf("addresses = %v and %v, want them in address order", alice.IPs[0].Address, alice.IPs[1].Address)
	}
	if !alice.IPs[0].LastSeen.Before(alice.IPs[1].LastSeen) {
		t.Errorf("last seen of .7 = %v, of .9 = %v: .9 began last", alice.IPs[0].LastSeen, alice.IPs[1].LastSeen)
	}
	if users[1].Conversations != 1 || len(users[1].IPs) != 1 {
		t.Errorf("bob = %+v", users[1])
	}
}

func TestAnAddressIsForgottenWhenItsLastConnectionEnds(t *testing.T) {
	registry := trackingRegistry()
	first := registry.TrackConversation(ConversationInfo{User: "alice", Source: sourceEndpoint(t, "203.0.113.7")}, nil)
	second := registry.TrackConversation(ConversationInfo{User: "alice", Source: sourceEndpoint(t, "203.0.113.7")}, nil)

	first.End()
	if users := registry.OnlineUsers(); len(users) != 1 || len(users[0].IPs) != 1 {
		t.Fatalf("users = %+v, want alice still online from the address", users)
	}
	second.End()
	if users := registry.OnlineUsers(); len(users) != 0 {
		t.Errorf("users = %+v, want nobody once the last conversation ended: nothing is kept", users)
	}
}

func TestConcurrentTrackingEndingAndReading(t *testing.T) {
	registry := trackingRegistry()
	var running sync.WaitGroup
	for worker := range 16 {
		running.Add(1)
		go func() {
			defer running.Done()
			for range 200 {
				conversation := registry.TrackConversation(ConversationInfo{User: "alice", Source: sourceEndpoint(t, "203.0.113.7"), InboundTag: "socks-in"}, nil)
				conversation.Bytes().AddUplinkBytes(uint64(worker))
				_ = registry.LiveConversations()
				_ = registry.OnlineUsers()
				conversation.End()
			}
		}()
	}
	running.Wait()

	if live := registry.LiveConversations(); len(live) != 0 {
		t.Errorf("%d conversations left after every worker ended its own", len(live))
	}
}

func BenchmarkTrackAndEndAConnection(b *testing.B) {
	registry := trackingRegistry()
	info := ConversationInfo{Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}, Rule: "default"}
	b.ReportAllocs()
	for b.Loop() {
		registry.TrackConversation(info, nil).End()
	}
}

func BenchmarkTrackAndEndAConnectionParallel(b *testing.B) {
	registry := trackingRegistry()
	info := ConversationInfo{Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}, Rule: "default"}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			registry.TrackConversation(info, nil).End()
		}
	})
}

func BenchmarkTrackingOffCostsNothing(b *testing.B) {
	registry := newTestRegistry(Config{})
	info := ConversationInfo{Network: spec.NetworkTCP, InboundTag: "socks-in"}
	b.ReportAllocs()
	for b.Loop() {
		registry.TrackConversation(info, nil).End()
	}
}

func BenchmarkLiveConversationsListOf10000(b *testing.B) {
	registry := trackingRegistry()
	for range 10000 {
		registry.TrackConversation(ConversationInfo{InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}}, nil)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = registry.LiveConversations()
	}
}

func BenchmarkLiveConversationMemory(b *testing.B) {
	info := ConversationInfo{Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}, Rule: "routing.rules[3]"}
	for b.Loop() {
		registry := trackingRegistry()
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		const entries = 10000
		for range entries {
			registry.TrackConversation(info, nil)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/entries, "bytes/entry")
		runtime.KeepAlive(registry)
	}
}
