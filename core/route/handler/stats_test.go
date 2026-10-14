package handler_test

import (
	"context"
	"io"
	"maps"
	"net"
	"syscall"
	"testing"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/outbound/group"
	"github.com/we-apranik/apranik/core/route/handler"
	"github.com/we-apranik/apranik/core/route/router"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

func newStatsRegistry(outbounds ...stats.OutboundDescription) *stats.Registry {
	return stats.NewRegistry(stats.Config{InboundTags: []string{"in"}, Outbounds: outbounds})
}

// Sends request one way and response the other through the handler, then
// closes both ends and waits for the handler to return.
func relayOneExchange(t *testing.T, trafficHandler *handler.Handler, destinationSide <-chan net.Conn, metadata spec.Metadata, request, response int) {
	t.Helper()
	clientSide, inboundSide := net.Pipe()
	handled := make(chan error, 1)
	go func() {
		handled <- trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, metadata)
	}()

	go clientSide.Write(make([]byte, request))
	remote := <-destinationSide
	if _, err := io.ReadFull(remote, make([]byte, request)); err != nil {
		t.Fatal(err)
	}
	go remote.Write(make([]byte, response))
	if _, err := io.ReadFull(clientSide, make([]byte, response)); err != nil {
		t.Fatal(err)
	}
	clientSide.Close()
	remote.Close()
	<-handled
}

func TestStreamBytesAndConnectionsAreCountedUnderInboundUserAndOutbound(t *testing.T) {
	registry := newStatsRegistry(stats.OutboundDescription{Tag: "out"})
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})

	relayOneExchange(t, trafficHandler, out.dialed,
		spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in", User: "alice"}, 100, 300)

	snapshot := viewOf(registry)
	for name, traffic := range map[string]stats.Traffic{
		"inbound in":   snapshot.Inbounds["in"].Traffic,
		"user alice":   snapshot.Users["alice"],
		"outbound out": snapshot.Outbounds["out"].Traffic,
	} {
		if traffic.UplinkBytes != 100 || traffic.DownlinkBytes != 300 {
			t.Errorf("%s: uplink %d, downlink %d, want 100 and 300", name, traffic.UplinkBytes, traffic.DownlinkBytes)
		}
		if traffic.TCP.TotalConnections != 1 || traffic.TCP.ActiveConnections != 0 {
			t.Errorf("%s: %d total, %d active connections, want 1 and 0", name, traffic.TCP.TotalConnections, traffic.TCP.ActiveConnections)
		}
	}
}

func TestAConnectionWithoutAnAuthenticatedUserCreatesNoUserCounters(t *testing.T) {
	registry := newStatsRegistry(stats.OutboundDescription{Tag: "out"})
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})

	relayOneExchange(t, trafficHandler, out.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"}, 10, 10)

	if users := registry.UserTraffic(); len(users) != 0 {
		t.Errorf("users = %v, want none", users)
	}
}

func TestAStreamThroughAGroupIsCountedUnderTheGroupAndTheMemberThatCarriedIt(t *testing.T) {
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
	registry := newStatsRegistry(
		stats.OutboundDescription{Tag: "auto", IsGroup: true},
		stats.OutboundDescription{Tag: "first"},
		stats.OutboundDescription{Tag: "second"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(groupOutbound), handler.Config{Stats: registry})

	relayOneExchange(t, trafficHandler, first.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"}, 50, 80)

	snapshot := viewOf(registry)
	if got := snapshot.Outbounds["auto"]; !got.IsGroup || got.UplinkBytes != 50 || got.DownlinkBytes != 80 {
		t.Errorf("group = %+v, want the groupOutbound's 50 up and 80 down", got)
	}
	if got := snapshot.Outbounds["first"]; got.UplinkBytes != 50 || got.DownlinkBytes != 80 || got.TCP.TotalConnections != 1 {
		t.Errorf("first = %+v, want the connection it carried", got)
	}
	if got := snapshot.Outbounds["second"]; got.UplinkBytes != 0 || got.TCP.TotalConnections != 0 {
		t.Errorf("second = %+v, want nothing: it did not carry the connection", got)
	}
}

func TestABlockedStreamIsCountedAsRefusedOnItsInbound(t *testing.T) {
	registry := newStatsRegistry()
	clientSide, inboundSide := net.Pipe()
	defer clientSide.Close()

	trafficHandler := handler.New(blockEverything(t), handler.Config{Stats: registry})
	if err := trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"}); err != nil {
		t.Fatal(err)
	}

	if got := registry.InboundTraffic()["in"]; got.RefusedConnections != 1 || got.TCP.TotalConnections != 0 || got.RefusalsByReason["blocked_by_rule"] != 1 {
		t.Errorf("inbound = %+v, want 1 refused for blocked_by_rule and no connection", got)
	}
}

type failingOutbound struct {
	capturingOutbound
	err error
}

func (o failingOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return nil, o.err
}

func TestAFailedDialIsCountedOnTheOutbound(t *testing.T) {
	registry := newStatsRegistry(stats.OutboundDescription{Tag: "out"})
	clientSide, inboundSide := net.Pipe()
	defer clientSide.Close()

	trafficHandler := handler.New(router.NewWithDefaultOutbound(failingOutbound{capturingOutbound{tag: "out"}, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}), handler.Config{Stats: registry})
	if err := trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in"}); err == nil {
		t.Fatal("HandleStream succeeded with an outbound that cannot dial")
	}

	snapshot := viewOf(registry)
	if got := snapshot.Outbounds["out"].DialFailuresByReason; got["refused"] != 1 {
		t.Errorf("dial failures by reason = %v, want one refused", got)
	}
	if snapshot.Outbounds["out"].DialFailures != 1 || snapshot.Outbounds["out"].TCP.TotalConnections != 0 {
		t.Errorf("outbound = %+v, want 1 dial failure and no connection", snapshot.Outbounds["out"])
	}
}

func TestEachConnectionCountsAHitOnTheRuleThatDecidedIt(t *testing.T) {
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	ruleLocations := []string{router.RuleLocation(0), router.RuleLocation(1), router.DefaultRuleLocation}
	registry := stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"}, Outbounds: []stats.OutboundDescription{{Tag: "out"}}, RuleLocations: ruleLocations,
	})
	routing, err := router.New(router.Config{
		Routing: blueprint.Routing{
			Rules: []blueprint.Rule{
				{Match: blueprint.Match{Ports: []blueprint.PortRange{{First: 443, Last: 443}}}, Action: blueprint.Action{Route: &blueprint.RouteAction{Outbound: "out"}}},
				{Match: blueprint.Match{Ports: []blueprint.PortRange{{First: 25, Last: 25}}}, Action: blueprint.Action{Block: true}},
			},
			DefaultAction: blueprint.Action{Route: &blueprint.RouteAction{Outbound: "out"}},
		},
		Outbounds: map[string]component.Outbound{"out": out},
	})
	if err != nil {
		t.Fatal(err)
	}
	trafficHandler := handler.New(routing, handler.Config{Stats: registry})

	// A connection the router lets through is dialed, so the test takes the
	// dialed stream and closes it; a blocked one is never dialed.
	connect := func(port uint16, isDialed bool) {
		destination, _ := spec.NewDestinationFromDomain("example.com", port)
		clientSide, inboundSide := net.Pipe()
		clientSide.Close()
		if isDialed {
			go func() { (<-out.dialed).Close() }()
		}
		trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in", Destination: destination})
	}
	connect(443, true)
	connect(443, true)
	connect(25, false)
	connect(8080, true)

	want := map[string]uint64{"routing.rules[0]": 2, "routing.rules[1]": 1, "default": 1}
	if got := registry.RuleHits(); !maps.Equal(got, want) {
		t.Errorf("rule hits = %v, want %v", got, want)
	}
}

func TestNoRegistryMeansNothingIsCounted(t *testing.T) {
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{})

	relayOneExchange(t, trafficHandler, out.dialed, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in", User: "alice"}, 10, 10)
}

// Everything the registry's fixed-size reads return, for assertions that look
// at several kinds at once.
type trafficView struct {
	Inbounds  map[string]stats.InboundTraffic
	Outbounds map[string]stats.OutboundTraffic
	Users     map[string]stats.Traffic
}

func viewOf(registry *stats.Registry) trafficView {
	return trafficView{
		Inbounds:  registry.InboundTraffic(),
		Outbounds: registry.OutboundTraffic(),
		Users:     registry.UserTraffic(),
	}
}
