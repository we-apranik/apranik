package handler

import (
	"net/netip"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

func newLiveEntriesForTest(t *testing.T, idleTimeout time.Duration, maxEntries int) (*udpSessionLiveEntries, *stats.Registry) {
	t.Helper()
	registry := stats.NewRegistry(stats.Config{InboundTags: []string{"in"}, TrackLiveConnections: true})
	live := newUDPSessionLiveEntries(registry, stats.ConnectionInfo{Network: spec.NetworkUDP, InboundTag: "in", User: "alice"},
		func(routed component.Outbound) []string { return []string{"out"} }, idleTimeout, maxEntries)
	sweeper := newPeriodicSweeper(sweepIntervalFor(idleTimeout))
	sweeper.add(live)
	t.Cleanup(func() {
		sweeper.remove(live)
		live.close()
	})
	return live, registry
}

func decisionForTest() component.Decision {
	return component.Decision{Outbound: stubOutbound{}, Rule: "default"}
}

func TestADestinationIsListedFromItsFirstDatagramWithItsBytes(t *testing.T) {
	live, registry := newLiveEntriesForTest(t, time.Minute, 10)
	target := destinationForTest(t, "192.0.2.1", 53)

	live.uplinkAccepted(target, decisionForTest(), 30)
	if !live.downlinkReceived(target, 50) {
		t.Fatal("a reply found no entry for a listed destination")
	}

	entries := registry.LiveConnections()
	if len(entries) != 1 || entries[0].UplinkBytes != 30 || entries[0].DownlinkBytes != 50 ||
		entries[0].Info.User != "alice" || entries[0].Info.Rule != "default" || entries[0].Info.Destination != target {
		t.Errorf("entries = %+v, want one with 30 up, 50 down, alice, the rule and the destination", entries)
	}
}

func TestAReplyFromAnUnlistedDestinationFindsNoEntryAndCreatesNone(t *testing.T) {
	live, registry := newLiveEntriesForTest(t, time.Minute, 10)

	if live.downlinkReceived(destinationForTest(t, "192.0.2.1", 53), 10) {
		t.Error("a reply found an entry that does not exist")
	}
	if len(registry.LiveConnections()) != 0 {
		t.Error("a reply from an unknown destination listed an entry")
	}
}

func TestAReplyThatBeatsTheUplinkReportStillListsTheEntry(t *testing.T) {
	live, registry := newLiveEntriesForTest(t, time.Minute, 10)
	target := destinationForTest(t, "192.0.2.1", 53)

	live.downlinkReceivedFromRoutedDestination(target, decisionForTest(), 7)
	live.uplinkAccepted(target, decisionForTest(), 3)

	entries := registry.LiveConnections()
	if len(entries) != 1 || entries[0].UplinkBytes != 3 || entries[0].DownlinkBytes != 7 {
		t.Errorf("entries = %+v, want one entry with both directions", entries)
	}
}

func TestTheEntryUsedLongestAgoEndsWhenTheSessionListsTooMany(t *testing.T) {
	live, registry := newLiveEntriesForTest(t, time.Minute, 2)
	first, second, third := destinationForTest(t, "192.0.2.1", 53), destinationForTest(t, "192.0.2.2", 53), destinationForTest(t, "192.0.2.3", 53)

	live.uplinkAccepted(first, decisionForTest(), 1)
	live.uplinkAccepted(second, decisionForTest(), 1)
	live.downlinkReceived(first, 1) // a reply is use too: second is now the oldest
	live.uplinkAccepted(third, decisionForTest(), 1)

	listed := map[spec.Destination]bool{}
	for _, entry := range registry.LiveConnections() {
		listed[entry.Info.Destination] = true
	}
	if len(listed) != 2 || !listed[first] || !listed[third] {
		t.Errorf("listed = %v, want first and third", listed)
	}
}

func TestAnEntryThatKeepsReceivingRepliesDoesNotGoIdle(t *testing.T) {
	live, registry := newLiveEntriesForTest(t, 200*time.Millisecond, 10)
	target := destinationForTest(t, "192.0.2.1", 53)
	live.uplinkAccepted(target, decisionForTest(), 1)

	for range 12 {
		time.Sleep(50 * time.Millisecond)
		if !live.downlinkReceived(target, 1) {
			t.Fatal("the entry ended while replies kept arriving")
		}
	}
	if len(registry.LiveConnections()) != 1 {
		t.Fatal("the entry is not listed after 600 ms of replies against a 200 ms idle time")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(registry.LiveConnections()) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the entry stayed listed after the destination went silent")
}

func TestClosingTheSessionsLiveEntriesEndsEveryEntry(t *testing.T) {
	live, registry := newLiveEntriesForTest(t, time.Minute, 10)
	for port := uint16(1); port <= 4; port++ {
		live.uplinkAccepted(destinationForTest(t, "192.0.2.1", port), decisionForTest(), 1)
	}

	live.close()

	if len(registry.LiveConnections()) != 0 {
		t.Errorf("%d entries left after the session ended", len(registry.LiveConnections()))
	}
}

func BenchmarkLiveEntryUplinkAccepted(b *testing.B) {
	registry := stats.NewRegistry(stats.Config{InboundTags: []string{"in"}, TrackLiveConnections: true})
	live := newUDPSessionLiveEntries(registry, stats.ConnectionInfo{InboundTag: "in"},
		func(component.Outbound) []string { return []string{"out"} }, time.Minute, 1024)
	b.Cleanup(live.close)
	target, err := spec.NewDestinationFromIP(netip.MustParseAddr("192.0.2.1"), 53)
	if err != nil {
		b.Fatal(err)
	}
	decision := decisionForTest()
	live.uplinkAccepted(target, decision, 1)

	b.ReportAllocs()
	for b.Loop() {
		live.uplinkAccepted(target, decision, 512)
	}
}
