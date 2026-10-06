package handler_test

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

// An outbound whose datagram answers every datagram it is sent with the same
// bytes, from the destination it was sent to.
type echoUDPOutbound struct{ tag string }

func (o echoUDPOutbound) Tag() string              { return o.tag }
func (o echoUDPOutbound) Networks() []spec.Network { return []spec.Network{spec.NetworkUDP} }
func (o echoUDPOutbound) Close() error             { return nil }
func (o echoUDPOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return nil, spec.ErrUnsupportedNetwork
}
func (o echoUDPOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return &echoDatagram{echoes: make(chan echoedDatagram, 64), done: make(chan struct{})}, nil
}

type echoedDatagram struct {
	payload []byte
	from    spec.Destination
}

type echoDatagram struct {
	echoes    chan echoedDatagram
	closeOnce sync.Once
	done      chan struct{}
}

func (d *echoDatagram) WriteTo(payload []byte, to spec.Destination) (int, error) {
	select {
	case d.echoes <- echoedDatagram{payload: slices.Clone(payload), from: to}:
		return len(payload), nil
	case <-d.done:
		return 0, net.ErrClosed
	}
}

func (d *echoDatagram) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	select {
	case echoed := <-d.echoes:
		return copy(buffer, echoed.payload), echoed.from, nil
	case <-d.done:
		return 0, spec.Destination{}, net.ErrClosed
	}
}

func (d *echoDatagram) Close() error {
	d.closeOnce.Do(func() { close(d.done) })
	return nil
}

func (d *echoDatagram) LocalAddr() net.Addr { return nil }

// A member of a group, carrying UDP like echoUDPOutbound.
func newUDPGroup(t *testing.T, members ...component.Outbound) component.Outbound {
	t.Helper()
	group, err := outbound.NewGroup(outbound.GroupConfig{
		Tag:     "auto",
		Members: members,
		Probe:   outbound.ProbeConfig{URL: "http://probe.example/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { group.Close() })
	return group
}

func udpTarget(t *testing.T, host string, port uint16) spec.Destination {
	t.Helper()
	target, err := spec.NewDestinationFromIP(netip.MustParseAddr(host), port)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func udpTrackingRegistry(outbounds ...stats.OutboundDescription) *stats.Registry {
	return stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"}, Outbounds: outbounds, TrackLiveConnections: true,
		RuleLocations: []string{router.DefaultRuleLocation},
	})
}

type udpSessionUnderTest struct {
	client  *clientDatagram
	ended   chan error
	cancel  context.CancelFunc
	payload int
}

// Runs one UDP session through the handler; each packet the test sends is the
// size of label.
func startUDPSession(t *testing.T, trafficHandler *handler.Handler, metadata spec.Metadata, label string) *udpSessionUnderTest {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	session := &udpSessionUnderTest{client: newClientDatagram(label), ended: make(chan error, 1), cancel: cancel, payload: len(label)}
	go func() { session.ended <- trafficHandler.HandleDatagram(ctx, session.client, metadata) }()
	t.Cleanup(func() {
		cancel()
		session.client.Close()
		select {
		case <-session.ended:
		case <-time.After(5 * time.Second):
			t.Error("the UDP session did not end")
		}
	})
	return session
}

// Sends one packet and waits for its echo to reach the client.
func (s *udpSessionUnderTest) exchange(t *testing.T, to spec.Destination) {
	t.Helper()
	s.client.outgoing <- to
	select {
	case <-s.client.returned:
	case <-time.After(5 * time.Second):
		t.Fatal("no reply reached the client")
	}
}

func udpMetadata(user string) spec.Metadata {
	source, _ := spec.NewEndpoint(netip.MustParseAddr("203.0.113.7"), 50000)
	return spec.Metadata{Network: spec.NetworkUDP, InboundTag: "in", User: user, Transport: spec.TransportMetadata{SourceEndpoint: source}}
}

func TestUDPDatagramsAreCountedAsUDPUnderInboundUserAndOutbound(t *testing.T) {
	registry := newStatsRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry, UDPCountFlushInterval: 5 * time.Millisecond})
	session := startUDPSession(t, trafficHandler, udpMetadata("alice"), "twelve bytes")

	target := udpTarget(t, "192.0.2.1", 53)
	for range 3 {
		session.exchange(t, target)
	}
	eventually(t, "the three datagrams are added to the shared counters", func() bool {
		return registry.OutboundTraffic()["out"].UDP.DownlinkPackets == 3
	})

	view := viewOf(registry)
	for name, traffic := range map[string]stats.Traffic{
		"inbound in":   view.Inbounds["in"].Traffic,
		"user alice":   view.Users["alice"],
		"outbound out": view.Outbounds["out"].Traffic,
	} {
		want := stats.UDPTraffic{UplinkBytes: 36, DownlinkBytes: 36, UplinkPackets: 3, DownlinkPackets: 3, Sessions: 1}
		if traffic.UDP != want {
			t.Errorf("%s UDP = %+v, want %+v", name, traffic.UDP, want)
		}
		if traffic.TCP != (stats.TCPTraffic{}) {
			t.Errorf("%s TCP = %+v, want nothing: UDP is counted apart", name, traffic.TCP)
		}
		if traffic.UplinkBytes != 36 || traffic.DownlinkBytes != 36 {
			t.Errorf("%s sum = %d up, %d down, want 36 and 36", name, traffic.UplinkBytes, traffic.DownlinkBytes)
		}
	}
	if totals := registry.TotalTraffic(); totals.UDP.UplinkBytes != 36 || totals.TCP.UplinkBytes != 0 || totals.All.DownlinkBytes != 36 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestEachDestinationOfASessionIsOneLiveEntryWithItsOwnBytes(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry})
	session := startUDPSession(t, trafficHandler, udpMetadata("alice"), "abcd")

	first, second := udpTarget(t, "192.0.2.1", 53), udpTarget(t, "192.0.2.2", 443)
	session.exchange(t, first)
	session.exchange(t, first)
	session.exchange(t, second)

	eventually(t, "both destinations are listed with their bytes", func() bool {
		live := registry.LiveConnections()
		return len(live) == 2 && live[0].DownlinkBytes+live[1].DownlinkBytes == 12
	})
	live := registry.LiveConnections()
	byDestination := map[string]stats.ConnectionSnapshot{}
	for _, entry := range live {
		byDestination[entry.Info.Destination.String()] = entry
		if entry.Info.Network != spec.NetworkUDP || entry.Info.InboundTag != "in" || entry.Info.User != "alice" ||
			entry.Info.Rule != router.DefaultRuleLocation || !slices.Equal(entry.Info.OutboundTags, []string{"out"}) ||
			entry.Info.Source.Addr().String() != "203.0.113.7" {
			t.Errorf("entry info = %+v", entry.Info)
		}
	}
	if got := byDestination[first.String()]; got.UplinkBytes != 8 || got.DownlinkBytes != 8 {
		t.Errorf("first destination = %d up, %d down, want 8 and 8: two datagrams of four bytes", got.UplinkBytes, got.DownlinkBytes)
	}
	if got := byDestination[second.String()]; got.UplinkBytes != 4 || got.DownlinkBytes != 4 {
		t.Errorf("second destination = %d up, %d down, want 4 and 4", got.UplinkBytes, got.DownlinkBytes)
	}
	if users := registry.OnlineUsers(); len(users) != 1 || users[0].Name != "alice" || users[0].Connections != 2 {
		t.Errorf("online users = %+v, want alice online through her two live UDP entries", users)
	}
}

func TestAUDPLiveEntryEndsWhenItsDestinationHasBeenSilentAndWhenTheSessionEnds(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}),
		handler.Config{Stats: registry, UDPSessionLiveConnectionsIdleTimeout: 200 * time.Millisecond})
	session := startUDPSession(t, trafficHandler, udpMetadata("alice"), "abcd")

	session.exchange(t, udpTarget(t, "192.0.2.1", 53))
	eventually(t, "the destination is listed", func() bool { return len(registry.LiveConnections()) == 1 })
	eventually(t, "the idle destination is forgotten", func() bool { return len(registry.LiveConnections()) == 0 })

	session.exchange(t, udpTarget(t, "192.0.2.1", 53))
	eventually(t, "the destination is listed again after a new packet", func() bool { return len(registry.LiveConnections()) == 1 })

	session.cancel()
	eventually(t, "the entry ends with the session", func() bool { return len(registry.LiveConnections()) == 0 })
}

func TestASessionNeverListsMoreDestinationsThanItsLiveEntriesHold(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}),
		handler.Config{Stats: registry, UDPSessionLiveConnectionsMaxEntries: 2, UDPCountFlushInterval: 5 * time.Millisecond})
	session := startUDPSession(t, trafficHandler, udpMetadata(""), "abcd")

	for host := 1; host <= 5; host++ {
		session.exchange(t, udpTarget(t, "192.0.2.1", uint16(host)))
	}

	eventually(t, "only the two destinations used last are listed", func() bool { return len(registry.LiveConnections()) == 2 })
	for _, entry := range registry.LiveConnections() {
		if port := entry.Info.Destination.Port(); port != 4 && port != 5 {
			t.Errorf("destination port %d is listed, want only the last two used (4 and 5)", port)
		}
	}
	eventually(t, "all five datagrams are counted", func() bool { return registry.InboundTraffic()["in"].UDP.UplinkPackets == 5 })
}

func TestForgettingARoutingDecisionLeavesTheDestinationsLiveEntry(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}),
		handler.Config{Stats: registry, UDPSessionRouteCacheMaxEntries: 1})
	session := startUDPSession(t, trafficHandler, udpMetadata(""), "abcd")

	for host := 1; host <= 3; host++ {
		session.exchange(t, udpTarget(t, "192.0.2.1", uint16(host)))
	}

	eventually(t, "all three destinations are listed", func() bool { return len(registry.LiveConnections()) == 3 })
	if hits := registry.RuleHits()[router.DefaultRuleLocation]; hits != 3 {
		t.Errorf("rule hits = %d, want 3", hits)
	}
	session.exchange(t, udpTarget(t, "192.0.2.1", 1)) // its decision was forgotten: routed again
	eventually(t, "the destination routed again is a fourth hit", func() bool {
		return registry.RuleHits()[router.DefaultRuleLocation] == 4
	})
	if len(registry.LiveConnections()) != 3 {
		t.Errorf("%d live entries, want 3: routing a destination again is not a new entry", len(registry.LiveConnections()))
	}
}

func TestEveryDecisionCountsOneRuleHitNotEveryPacket(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry})
	session := startUDPSession(t, trafficHandler, udpMetadata(""), "abcd")

	first, second := udpTarget(t, "192.0.2.1", 53), udpTarget(t, "192.0.2.2", 53)
	for range 5 {
		session.exchange(t, first)
	}
	session.exchange(t, second)

	if hits := registry.RuleHits()[router.DefaultRuleLocation]; hits != 2 {
		t.Errorf("rule hits = %d, want 2: one per routed destination, not one per packet", hits)
	}
}

func TestABlockedUDPDestinationCountsADropPerPacketAndHasNoEntry(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	blocked := udpTarget(t, "192.0.2.99", 9)
	blocking, err := router.New(router.Config{
		Routing: blueprint.Routing{
			Rules: []blueprint.Rule{{
				Match:  blueprint.Match{Ports: []blueprint.PortRange{{First: 9, Last: 9}}},
				Action: blueprint.Action{Block: true},
			}},
			DefaultAction: blueprint.Action{Route: &blueprint.RouteAction{Outbound: "out"}},
		},
		Outbounds: map[string]component.Outbound{"out": echoUDPOutbound{tag: "out"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	trafficHandler := handler.New(blocking, handler.Config{Stats: registry, UDPCountFlushInterval: 5 * time.Millisecond})
	session := startUDPSession(t, trafficHandler, udpMetadata(""), "abcd")

	for range 3 {
		session.client.outgoing <- blocked
	}
	session.exchange(t, udpTarget(t, "192.0.2.1", 53))

	eventually(t, "each blocked packet is a drop", func() bool {
		return registry.InboundTraffic()["in"].DatagramDropsByReason["blocked_by_rule"] == 3
	})
	live := registry.LiveConnections()
	if len(live) != 1 || live[0].Info.Destination.String() == blocked.String() {
		t.Errorf("live entries = %+v, want only the allowed destination", live)
	}
	eventually(t, "the allowed packet is counted", func() bool { return registry.InboundTraffic()["in"].UDP.UplinkPackets >= 1 })
	if registry.InboundTraffic()["in"].UDP.UplinkPackets != 1 {
		t.Errorf("a blocked packet was counted as carried")
	}
}

func TestTwoUDPSessionsToOneDestinationHaveTheirOwnEntries(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry})
	first := startUDPSession(t, trafficHandler, udpMetadata("alice"), "aa")
	second := startUDPSession(t, trafficHandler, udpMetadata("bob"), "bbbb")

	target := udpTarget(t, "192.0.2.1", 53)
	first.exchange(t, target)
	second.exchange(t, target)

	eventually(t, "two entries", func() bool { return len(registry.LiveConnections()) == 2 })
	if sessions := registry.InboundTraffic()["in"].UDP.Sessions; sessions != 2 {
		t.Errorf("sessions = %d, want 2", sessions)
	}
	bytesByUser := map[string]uint64{}
	for _, entry := range registry.LiveConnections() {
		bytesByUser[entry.Info.User] = entry.UplinkBytes
	}
	if bytesByUser["alice"] != 2 || bytesByUser["bob"] != 4 {
		t.Errorf("bytes by user = %v, want alice 2 and bob 4: sessions never share an entry", bytesByUser)
	}
}

func TestAUDPSessionThroughAGroupIsCountedUnderTheGroupAndTheMember(t *testing.T) {
	member := echoUDPOutbound{tag: "member"}
	group := newUDPGroup(t, member)
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "auto", IsGroup: true}, stats.OutboundDescription{Tag: "member"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(group), handler.Config{Stats: registry, UDPCountFlushInterval: 5 * time.Millisecond})
	session := startUDPSession(t, trafficHandler, udpMetadata(""), "abcd")

	session.exchange(t, udpTarget(t, "192.0.2.1", 53))
	eventually(t, "the datagrams are added to the shared counters", func() bool {
		return registry.OutboundTraffic()["member"].UDP.DownlinkPackets == 1
	})

	for _, tag := range []string{"auto", "member"} {
		if got := registry.OutboundTraffic()[tag].UDP; got.UplinkBytes != 4 || got.DownlinkBytes != 4 || got.Sessions != 1 {
			t.Errorf("%s UDP = %+v, want 4 up, 4 down, 1 session", tag, got)
		}
	}
	eventually(t, "the entry is listed", func() bool { return len(registry.LiveConnections()) == 1 })
	if tags := registry.LiveConnections()[0].Info.OutboundTags; !slices.Equal(tags, []string{"auto", "member"}) {
		t.Errorf("entry outbound tags = %v, want [auto member]", tags)
	}
}

func TestNothingIsListedForUDPWhenLiveConnectionsAreNotTracked(t *testing.T) {
	registry := newStatsRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry, UDPCountFlushInterval: 5 * time.Millisecond})
	session := startUDPSession(t, trafficHandler, udpMetadata(""), "abcd")

	session.exchange(t, udpTarget(t, "192.0.2.1", 53))
	eventually(t, "the datagram is counted", func() bool { return registry.InboundTraffic()["in"].UDP.DownlinkPackets == 1 })

	if registry.TracksLiveConnections() || len(registry.LiveConnections()) != 0 {
		t.Error("UDP entries were listed with tracking off")
	}
	if got := registry.InboundTraffic()["in"].UDP; got.UplinkPackets != 1 || got.DownlinkPackets != 1 {
		t.Errorf("UDP = %+v: the counters still count with tracking off", got)
	}
}

// The same datagrams are counted in three places (the session's keys, the
// relay's report to the destination entry, the handler's entry); they must agree.
func TestTheLiveEntriesAddUpToWhatTheInboundCounted(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry, UDPCountFlushInterval: 5 * time.Millisecond})
	session := startUDPSession(t, trafficHandler, udpMetadata("alice"), "payload")

	for round := range 4 {
		for port := uint16(1); port <= 5; port++ {
			session.exchange(t, udpTarget(t, "192.0.2.1", port+uint16(round%2)))
		}
	}

	eventually(t, "the entries have every byte the inbound counted", func() bool {
		inbound := registry.InboundTraffic()["in"].UDP
		var up, down uint64
		for _, entry := range registry.LiveConnections() {
			up += entry.UplinkBytes
			down += entry.DownlinkBytes
		}
		return inbound.UplinkPackets == 20 && up == inbound.UplinkBytes && down == inbound.DownlinkBytes
	})
	inbound := registry.InboundTraffic()["in"].UDP
	if inbound.UplinkPackets != 20 || inbound.DownlinkPackets != 20 {
		t.Errorf("packets = %d up, %d down, want 20 each", inbound.UplinkPackets, inbound.DownlinkPackets)
	}
}
