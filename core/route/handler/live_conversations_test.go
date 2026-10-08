package handler_test

import (
	"context"
	"io"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/outbound/group"
	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

func trackingRegistry(outbounds ...stats.OutboundDescription) *stats.Registry {
	return stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"}, Outbounds: outbounds, TrackLiveConversations: true,
		RuleLocations: []string{router.DefaultRuleLocation},
	})
}

func eventually(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s never held", description)
}

// A connection held open: the test sends on it, looks at the registry, then
// ends it and waits for the handler to return.
type openConnection struct {
	client, remote  net.Conn
	handlerReturned chan error
}

func openThrough(t *testing.T, trafficHandler *handler.Handler, dialed <-chan net.Conn, metadata spec.Metadata) *openConnection {
	t.Helper()
	client, inboundSide := net.Pipe()
	connection := &openConnection{client: client, handlerReturned: make(chan error, 1)}
	go func() {
		connection.handlerReturned <- trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, metadata)
	}()
	select {
	case connection.remote = <-dialed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was never dialed")
	}
	return connection
}

func (c *openConnection) end(t *testing.T) {
	t.Helper()
	c.client.Close()
	c.remote.Close()
	select {
	case <-c.handlerReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return")
	}
}

func clientAt(t *testing.T, address string) spec.TransportMetadata {
	t.Helper()
	endpoint, err := spec.NewEndpoint(netip.MustParseAddr(address), 50000)
	if err != nil {
		t.Fatal(err)
	}
	return spec.TransportMetadata{SourceEndpoint: endpoint}
}

func TestALiveConversationIsListedWhileItRelaysAndGoneWhenItEnds(t *testing.T) {
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	registry := trackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})
	destination, _ := spec.NewDestinationFromDomain("example.com", 443)

	connection := openThrough(t, trafficHandler, out.dialed, spec.Metadata{
		Network: spec.NetworkTCP, InboundTag: "in", User: "alice", Destination: destination, Transport: clientAt(t, "203.0.113.7"),
	})

	go connection.client.Write(make([]byte, 100))
	io.ReadFull(connection.remote, make([]byte, 100))
	eventually(t, "the 100 bytes listed on the connection", func() bool {
		live := registry.LiveConversations()
		return len(live) == 1 && live[0].UplinkBytes == 100
	})

	live := registry.LiveConversations()[0]
	if live.Info.InboundTag != "in" || live.Info.User != "alice" || live.Info.Rule != router.DefaultRuleLocation ||
		live.Info.Source.Addr().String() != "203.0.113.7" || live.Info.Destination != destination ||
		!slices.Equal(live.Info.OutboundTags, []string{"out"}) {
		t.Errorf("info = %+v", live.Info)
	}
	if users := registry.OnlineUsers(); len(users) != 1 || users[0].Name != "alice" || len(users[0].IPs) != 1 || users[0].IPs[0].Address.String() != "203.0.113.7" {
		t.Errorf("online users = %+v, want alice from 203.0.113.7", users)
	}
	// The connection's own bytes are not added to the inbound twice: the
	// inbound's total still counts the payload once.
	if up := registry.TotalTraffic().All.UplinkBytes; up != 100 {
		t.Errorf("total uplink = %d, want 100", up)
	}

	connection.end(t)
	if live := registry.LiveConversations(); len(live) != 0 {
		t.Errorf("%d connections still listed after the handler returned", len(live))
	}
	if users := registry.OnlineUsers(); len(users) != 0 {
		t.Errorf("online users = %+v after the connection ended, want none", users)
	}
}

func TestALiveConversationThroughAGroupNamesTheGroupThenTheMember(t *testing.T) {
	first := capturingOutbound{tag: "first", dialed: make(chan net.Conn, 1)}
	second := capturingOutbound{tag: "second", dialed: make(chan net.Conn, 1)}
	groupOutbound, err := group.NewFallback(group.FallbackConfig{
		Tag:     "auto",
		Members: []component.Outbound{first, second},
		Probe:   group.ProbeConfig{URL: "http://probe.example/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer groupOutbound.Close()
	registry := trackingRegistry(
		stats.OutboundDescription{Tag: "auto", IsGroup: true},
		stats.OutboundDescription{Tag: "first"},
		stats.OutboundDescription{Tag: "second"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(groupOutbound), handler.Config{Stats: registry})

	connection := openThrough(t, trafficHandler, first.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"})
	defer connection.end(t)

	eventually(t, "the connection is listed", func() bool { return len(registry.LiveConversations()) == 1 })
	live := registry.LiveConversations()
	if len(live) != 1 || !slices.Equal(live[0].Info.OutboundTags, []string{"auto", "first"}) {
		t.Errorf("live = %+v, want one connection through auto then first", live)
	}
}

func TestAConversationWithNoAuthenticatedUserIsListedButIsNoOnlineUser(t *testing.T) {
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	registry := trackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})

	connection := openThrough(t, trafficHandler, out.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"})
	defer connection.end(t)

	eventually(t, "the connection is listed", func() bool { return len(registry.LiveConversations()) == 1 })
	if len(registry.LiveConversations()) != 1 || len(registry.OnlineUsers()) != 0 {
		t.Errorf("live = %d, online users = %v, want 1 and none", len(registry.LiveConversations()), registry.OnlineUsers())
	}
}

func TestNothingIsListedWhenLiveConversationsAreNotTracked(t *testing.T) {
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	registry := newStatsRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})

	connection := openThrough(t, trafficHandler, out.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in", User: "alice"})
	defer connection.end(t)

	if registry.IsLiveTrackingEnabled() || len(registry.LiveConversations()) != 0 {
		t.Error("connections were listed with tracking off")
	}
	eventually(t, "the connection is counted", func() bool { return registry.InboundTraffic()["in"].TCP.ActiveConnections == 1 })
	if got := registry.InboundTraffic()["in"]; got.TCP.ActiveConnections != 1 {
		t.Errorf("active connections = %d: the counters still count with tracking off", got.TCP.ActiveConnections)
	}
}

// slowOutbound takes a while to connect, so dial time has something to show.
type slowOutbound struct {
	capturingOutbound
	delay time.Duration
}

func (o slowOutbound) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	time.Sleep(o.delay)
	return o.capturingOutbound.DialStream(ctx, metadata)
}

func TestDialHealthTimesTheDialsOfTheOutboundRoutingNamed(t *testing.T) {
	const delay = 40 * time.Millisecond
	out := slowOutbound{capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}, delay}
	registry := stats.NewRegistry(stats.Config{InboundTags: []string{"in"}, Outbounds: []stats.OutboundDescription{{Tag: "out"}}, TimeDials: true})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})

	for range 2 {
		openThrough(t, trafficHandler, out.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"}).end(t)
	}

	got := registry.OutboundTraffic()["out"]
	if got.TimedDials != 2 || got.TotalDialTime < 2*delay || got.LastDialTime < delay {
		t.Errorf("outbound = %+v, want 2 timed dials of at least %v each", got, delay)
	}
	if got.TotalDialTime > 5*time.Second {
		t.Errorf("total dial time %v is implausible", got.TotalDialTime)
	}
}

func TestDialsAreNotTimedUnlessDialHealthIsOn(t *testing.T) {
	out := slowOutbound{capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}, 10 * time.Millisecond}
	registry := newStatsRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})

	openThrough(t, trafficHandler, out.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"}).end(t)

	if registry.TimesDials() {
		t.Error("the registry times dials by default")
	}
	if got := registry.OutboundTraffic()["out"]; got.TimedDials != 0 || got.TotalDialTime != 0 {
		t.Errorf("outbound = %+v, want no timing", got)
	}
}

func TestAFailedDialIsNotTimedAsAConnectedOne(t *testing.T) {
	registry := stats.NewRegistry(stats.Config{InboundTags: []string{"in"}, Outbounds: []stats.OutboundDescription{{Tag: "out"}}, TimeDials: true})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(failingOutbound{capturingOutbound{tag: "out"}, io.ErrClosedPipe}), handler.Config{Stats: registry})
	client, inboundSide := net.Pipe()
	defer client.Close()

	if err := trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"}); err == nil {
		t.Fatal("HandleStream succeeded with an outbound that cannot dial")
	}
	if got := registry.OutboundTraffic()["out"]; got.TimedDials != 0 || got.DialFailures != 1 {
		t.Errorf("outbound = %+v, want a failure and no timed dial", got)
	}
}
