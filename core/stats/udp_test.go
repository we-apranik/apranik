package stats

import (
	"sync"
	"testing"
)

func TestUDPAndTCPAreCountedApartAndSumTogether(t *testing.T) {
	registry := newTestRegistry(Config{})
	inbound := registry.Inbound("socks-in").Base()

	inbound.AddUplinkBytes(100)
	inbound.AddDownlinkBytes(1000)
	inbound.AddUplinkDatagram(30)
	inbound.AddUplinkDatagram(20)
	inbound.AddDownlinkDatagram(400)
	inbound.UDPSessionStarted()

	got := registry.InboundTraffic()["socks-in"]
	wantUDP := UDPTraffic{UplinkBytes: 50, DownlinkBytes: 400, UplinkPackets: 2, DownlinkPackets: 1, Sessions: 1}
	if got.UDP != wantUDP {
		t.Errorf("UDP = %+v, want %+v", got.UDP, wantUDP)
	}
	if got.TCP.UplinkBytes != 100 || got.TCP.DownlinkBytes != 1000 {
		t.Errorf("TCP = %+v: UDP bytes must not reach it", got.TCP)
	}
	if got.UplinkBytes != 150 || got.DownlinkBytes != 1400 {
		t.Errorf("sum = %d up and %d down, want 150 and 1400", got.UplinkBytes, got.DownlinkBytes)
	}
	if got.TCP.TotalConnections != 0 {
		t.Errorf("a UDP session counted as %d TCP connections", got.TCP.TotalConnections)
	}
}

func TestTotalTrafficSplitsByNetwork(t *testing.T) {
	registry := newTestRegistry(Config{})
	registry.Inbound("socks-in").Base().AddUplinkBytes(10)
	registry.Inbound("http-in").Base().AddUplinkDatagram(5)
	registry.Inbound("http-in").Base().AddDownlinkDatagram(7)
	registry.Outbound("server-a").Base().AddUplinkDatagram(999)

	totals := registry.TotalTraffic()
	want := TrafficTotals{
		All: NetworkBytes{UplinkBytes: 15, DownlinkBytes: 7},
		TCP: NetworkBytes{UplinkBytes: 10},
		UDP: NetworkBytes{UplinkBytes: 5, DownlinkBytes: 7},
	}
	if totals != want {
		t.Errorf("totals = %+v, want %+v", totals, want)
	}
}

func TestUDPMethodsDoNothingOnNilCounters(t *testing.T) {
	var counters *Counters
	counters.AddUplinkDatagram(10)
	counters.AddDownlinkDatagram(10)
	counters.UDPSessionStarted()
	var inbound *InboundCounters
	inbound.DatagramDroppedFor(InboundDropBlockedByRule)
	var outbound *OutboundCounters
	outbound.DatagramDroppedFor(OutboundDropSendFailed)

	if got := counters.Snapshot(); got != (Traffic{}) {
		t.Errorf("a nil Counters snapshot = %+v", got)
	}
}

func TestInboundDatagramDropsAreCountedByReason(t *testing.T) {
	registry := newTestRegistry(Config{})
	inbound := registry.Inbound("socks-in")
	for range 3 {
		inbound.DatagramDroppedFor(InboundDropBlockedByRule)
	}
	inbound.DatagramDroppedFor(InboundDropTooLargeFromClient)
	inbound.DatagramDroppedFor(InboundDatagramDropReason(200))

	got := registry.InboundTraffic()["socks-in"]
	want := map[string]uint64{"blocked_by_rule": 3, "too_large_from_client": 1, "other": 1}
	if got.DatagramDrops != 5 || len(got.DatagramDropsByReason) != len(want) {
		t.Fatalf("drops = %d, by reason %v, want 5 and %v", got.DatagramDrops, got.DatagramDropsByReason, want)
	}
	for reason, count := range want {
		if got.DatagramDropsByReason[reason] != count {
			t.Errorf("drops for %s = %d, want %d", reason, got.DatagramDropsByReason[reason], count)
		}
	}
}

func TestOutboundDatagramDropsAreCountedByReason(t *testing.T) {
	registry := newTestRegistry(Config{})
	outbound := registry.Outbound("server-a")
	outbound.DatagramDroppedFor(OutboundDropSendFailed)
	outbound.DatagramDroppedFor(OutboundDropReplyNotDelivered)
	outbound.DatagramDroppedFor(OutboundDropReplyNotDelivered)

	got := registry.OutboundTraffic()["server-a"]
	if got.DatagramDrops != 3 || got.DatagramDropsByReason["send_failed"] != 1 || got.DatagramDropsByReason["reply_not_delivered"] != 2 {
		t.Errorf("drops = %d, by reason %v", got.DatagramDrops, got.DatagramDropsByReason)
	}
	if registry.OutboundTraffic()["server-b"].DatagramDropsByReason != nil {
		t.Error("an outbound with no drops lists reasons")
	}
}

func TestConcurrentDatagramCountingAndReading(t *testing.T) {
	registry := newTestRegistry(Config{})
	const writers, datagramsPerWriter, size = 40, 300, 10

	var running sync.WaitGroup
	for range writers {
		running.Add(1)
		go func() {
			defer running.Done()
			inbound, outbound := registry.Inbound("socks-in").Base(), registry.Outbound("server-a").Base()
			for range datagramsPerWriter {
				inbound.AddUplinkDatagram(size)
				outbound.AddDownlinkDatagram(size)
				_ = registry.InboundTraffic()
				_ = registry.TotalTraffic()
			}
		}()
	}
	running.Wait()

	want := uint64(writers * datagramsPerWriter)
	if got := registry.InboundTraffic()["socks-in"].UDP; got.UplinkPackets != want || got.UplinkBytes != want*size {
		t.Errorf("inbound UDP = %+v, want %d packets", got, want)
	}
	if got := registry.OutboundTraffic()["server-a"].UDP; got.DownlinkPackets != want || got.DownlinkBytes != want*size {
		t.Errorf("outbound UDP = %+v, want %d packets", got, want)
	}
}

func BenchmarkAddUplinkDatagramFourCounters(b *testing.B) {
	registry := newTestRegistry(Config{})
	counters := []*Counters{
		registry.Inbound("socks-in").Base(), registry.User("alice"),
		registry.Outbound("auto").Base(), registry.Outbound("server-a").Base(),
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, counted := range counters {
			counted.AddUplinkDatagram(512)
		}
	}
}
