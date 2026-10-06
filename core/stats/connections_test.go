package stats

import (
	"net/netip"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

func trackingRegistry() *Registry {
	return newTestRegistry(Config{TrackLiveConnections: true})
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

	connection := registry.TrackConnection(ConnectionInfo{InboundTag: "socks-in"})
	if connection != nil || registry.TracksLiveConnections() {
		t.Fatalf("tracking is off, yet TrackConnection returned %v", connection)
	}
	connection.End()
	if connection.Bytes() != nil {
		t.Error("an untracked connection has counters")
	}
	if registry.LiveConnections() != nil || registry.OnlineUsers() != nil {
		t.Error("an untracked registry lists connections")
	}

	var nilRegistry *Registry
	if nilRegistry.TrackConnection(ConnectionInfo{}) != nil || nilRegistry.TracksLiveConnections() || nilRegistry.LiveConnections() != nil || nilRegistry.OnlineUsers() != nil {
		t.Error("a nil registry tracks connections")
	}
}

func TestALiveConnectionIsListedWithItsOwnBytesUntilItEnds(t *testing.T) {
	registry := trackingRegistry()
	connection := registry.TrackConnection(ConnectionInfo{
		Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice",
		Source: sourceEndpoint(t, "203.0.113.7"), Destination: destination(t, "example.com"),
		OutboundTags: []string{"auto", "server-a"}, Rule: "routing.rules[3]",
	})
	connection.Bytes().AddUplinkBytes(40)
	connection.Bytes().AddDownlinkBytes(900)

	live := registry.LiveConnections()
	if len(live) != 1 {
		t.Fatalf("%d live connections, want 1", len(live))
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

	connection.End()
	if live := registry.LiveConnections(); len(live) != 0 {
		t.Errorf("%d live connections after End, want 0", len(live))
	}
}

func TestConnectionIdentifiersAreASequenceAndNeverRepeat(t *testing.T) {
	registry := trackingRegistry()
	for range 1000 {
		registry.TrackConnection(ConnectionInfo{})
	}
	registry.TrackConnection(ConnectionInfo{}).End()

	live := registry.LiveConnections()
	if len(live) != 1000 {
		t.Fatalf("%d live connections, want 1000", len(live))
	}
	for index, connection := range live {
		if want := strconv.Itoa(index + 1); connection.ID != want {
			t.Fatalf("connection %d has id %q, want %q: oldest first, numbered in order", index, connection.ID, want)
		}
	}
	if next := registry.TrackConnection(ConnectionInfo{}); next.id != 1002 {
		t.Errorf("next id = %d, want 1002: an ended connection's number is not reused", next.id)
	}
}

func TestLiveConnectionsAreListedOldestFirst(t *testing.T) {
	registry := trackingRegistry()
	for _, tag := range []string{"first", "second", "third"} {
		registry.TrackConnection(ConnectionInfo{InboundTag: tag})
		time.Sleep(2 * time.Millisecond)
	}

	live := registry.LiveConnections()
	if len(live) != 3 || live[0].Info.InboundTag != "first" || live[1].Info.InboundTag != "second" || live[2].Info.InboundTag != "third" {
		t.Errorf("order = %+v", live)
	}
}

func TestAListOfLiveConnectionsIsACopy(t *testing.T) {
	registry := trackingRegistry()
	connection := registry.TrackConnection(ConnectionInfo{OutboundTags: []string{"auto", "server-a"}})
	connection.Bytes().AddUplinkBytes(1)

	live := registry.LiveConnections()
	live[0].Info.OutboundTags[0] = "changed"
	live[0].UplinkBytes = 999
	connection.Bytes().AddUplinkBytes(1)

	again := registry.LiveConnections()
	if again[0].Info.OutboundTags[0] != "auto" || again[0].UplinkBytes != 2 {
		t.Errorf("a change to a copy reached the live connection: %+v", again[0])
	}
}

func TestAnEndedConnectionLeavesNothingBehind(t *testing.T) {
	registry := trackingRegistry()
	for range 500 {
		registry.TrackConnection(ConnectionInfo{}).End()
	}
	left := 0
	for index := range registry.liveShards {
		shard := &registry.liveShards[index]
		shard.mu.Lock()
		left += len(shard.connections)
		shard.mu.Unlock()
	}
	if left != 0 {
		t.Errorf("%d entries left after every connection ended", left)
	}
}

func TestOnlineUsersGroupLiveConnectionsByUserAndSourceAddress(t *testing.T) {
	registry := trackingRegistry()
	track := func(user, address string) {
		registry.TrackConnection(ConnectionInfo{User: user, Source: sourceEndpoint(t, address)})
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
	if alice.Connections != 3 || len(alice.IPs) != 2 {
		t.Fatalf("alice = %+v, want 3 connections from 2 addresses", alice)
	}
	if alice.IPs[0].Address.String() != "203.0.113.7" || alice.IPs[1].Address.String() != "203.0.113.9" {
		t.Errorf("addresses = %v and %v, want them in address order", alice.IPs[0].Address, alice.IPs[1].Address)
	}
	if !alice.IPs[0].LastSeen.Before(alice.IPs[1].LastSeen) {
		t.Errorf("last seen of .7 = %v, of .9 = %v: .9 began last", alice.IPs[0].LastSeen, alice.IPs[1].LastSeen)
	}
	if users[1].Connections != 1 || len(users[1].IPs) != 1 {
		t.Errorf("bob = %+v", users[1])
	}
}

func TestAnAddressIsForgottenWhenItsLastConnectionEnds(t *testing.T) {
	registry := trackingRegistry()
	first := registry.TrackConnection(ConnectionInfo{User: "alice", Source: sourceEndpoint(t, "203.0.113.7")})
	second := registry.TrackConnection(ConnectionInfo{User: "alice", Source: sourceEndpoint(t, "203.0.113.7")})

	first.End()
	if users := registry.OnlineUsers(); len(users) != 1 || len(users[0].IPs) != 1 {
		t.Fatalf("users = %+v, want alice still online from the address", users)
	}
	second.End()
	if users := registry.OnlineUsers(); len(users) != 0 {
		t.Errorf("users = %+v, want nobody once the last connection ended: nothing is kept", users)
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
				connection := registry.TrackConnection(ConnectionInfo{User: "alice", Source: sourceEndpoint(t, "203.0.113.7"), InboundTag: "socks-in"})
				connection.Bytes().AddUplinkBytes(uint64(worker))
				_ = registry.LiveConnections()
				_ = registry.OnlineUsers()
				connection.End()
			}
		}()
	}
	running.Wait()

	if live := registry.LiveConnections(); len(live) != 0 {
		t.Errorf("%d connections left after every worker ended its own", len(live))
	}
}

func BenchmarkTrackAndEndAConnection(b *testing.B) {
	registry := trackingRegistry()
	info := ConnectionInfo{Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}, Rule: "default"}
	b.ReportAllocs()
	for b.Loop() {
		registry.TrackConnection(info).End()
	}
}

func BenchmarkTrackAndEndAConnectionParallel(b *testing.B) {
	registry := trackingRegistry()
	info := ConnectionInfo{Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}, Rule: "default"}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			registry.TrackConnection(info).End()
		}
	})
}

func BenchmarkTrackingOffCostsNothing(b *testing.B) {
	registry := newTestRegistry(Config{})
	info := ConnectionInfo{Network: spec.NetworkTCP, InboundTag: "socks-in"}
	b.ReportAllocs()
	for b.Loop() {
		registry.TrackConnection(info).End()
	}
}

func BenchmarkLiveConnectionsListOf10000(b *testing.B) {
	registry := trackingRegistry()
	for range 10000 {
		registry.TrackConnection(ConnectionInfo{InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}})
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = registry.LiveConnections()
	}
}

// The heap one live entry holds, measured over 10000 of them, to put a number
// on the memory a switched-on registry costs.
func BenchmarkLiveConnectionMemory(b *testing.B) {
	info := ConnectionInfo{Network: spec.NetworkTCP, InboundTag: "socks-in", User: "alice", OutboundTags: []string{"auto", "server-a"}, Rule: "routing.rules[3]"}
	for b.Loop() {
		registry := trackingRegistry()
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		const entries = 10000
		for range entries {
			registry.TrackConnection(info)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/entries, "bytes/entry")
		runtime.KeepAlive(registry)
	}
}
