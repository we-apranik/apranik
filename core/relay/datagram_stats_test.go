package relay_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/net/udp/session"
	"github.com/we-apranik/apranik/core/relay"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

func newRelayStats() *stats.Registry {
	return stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"},
		Outbounds:   []stats.OutboundDescription{{Tag: "freedom"}},
	})
}

type relayUnderTest struct {
	client     *clientDatagram
	udpSession *session.Session
	uplinks    chan int
	replies    chan int
}

func runCountingRelay(t *testing.T, registry *stats.Registry, pick func(spec.Destination) (component.Outbound, error)) *relayUnderTest {
	t.Helper()

	observed := &relayUnderTest{
		client:  newClientDatagram(),
		uplinks: make(chan int, 32),
		replies: make(chan int, 32),
	}
	observed.udpSession = session.New(context.Background(), session.Config{Metadata: spec.Metadata{InboundTag: "in"}, Stats: registry})
	finished := make(chan struct{})
	go func() {
		relay.Datagrams(context.Background(), relay.DatagramRelayConfig{
			ClientSide:    observed.client,
			ClientSession: observed.udpSession,
			OutboundForDestination: func(_ context.Context, destination spec.Destination) (relay.RoutedDestination, error) {
				outbound, err := pick(destination)
				if err != nil {
					return relay.RoutedDestination{}, err
				}
				return relay.RoutedDestination{Outbound: outbound, UplinkAccepted: func(size int) { observed.uplinks <- size }}, nil
			},
			ReplyReceived: func(_ spec.Destination, size int) { observed.replies <- size },
			Stats:         registry,
			InboundTag:    "in",
		})
		close(finished)
	}()
	t.Cleanup(func() {
		observed.client.Close()
		observed.udpSession.Close()
		<-finished
	})
	return observed
}

func inboundDrops(registry *stats.Registry) map[string]uint64 {
	return registry.InboundTraffic()["in"].DatagramDropsByReason
}

func eventuallyEqual(t *testing.T, description string, got func() uint64, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("%s = %d, want %d", description, got(), want)
}

func TestEachBlockedPacketIsADropOnTheInbound(t *testing.T) {
	registry := newRelayStats()
	blocked := destinationOf(t, "192.0.2.99", 9)
	observed := runCountingRelay(t, registry, func(spec.Destination) (component.Outbound, error) {
		return nil, relay.ErrDestinationBlocked
	})

	for range 3 {
		observed.client.outgoing <- blocked
	}

	eventuallyEqual(t, "blocked_by_rule drops", func() uint64 { return inboundDrops(registry)["blocked_by_rule"] }, 3)
}

func TestARoutingFailureIsADropOnTheInbound(t *testing.T) {
	registry := newRelayStats()
	observed := runCountingRelay(t, registry, func(spec.Destination) (component.Outbound, error) {
		return nil, errors.New("rules could not decide")
	})

	observed.client.outgoing <- destinationOf(t, "192.0.2.1", 53)

	eventuallyEqual(t, "routing_failed drops", func() uint64 { return inboundDrops(registry)["routing_failed"] }, 1)
}

func TestAClientPacketTooLargeIsADropOnTheInbound(t *testing.T) {
	registry := newRelayStats()
	observed := runCountingRelay(t, registry, func(spec.Destination) (component.Outbound, error) { return nil, nil })

	observed.client.readFailures <- spec.ErrMessageTooLong

	eventuallyEqual(t, "too_large_from_client drops", func() uint64 { return inboundDrops(registry)["too_large_from_client"] }, 1)
}

func TestAnOutboundThatCannotCarryUDPIsADropOnTheInboundOnly(t *testing.T) {
	registry := newRelayStats()
	tcpOnly := &fakeOutbound{tag: "freedom", networks: []spec.Network{spec.NetworkTCP}}
	observed := runCountingRelay(t, registry, func(spec.Destination) (component.Outbound, error) { return tcpOnly, nil })

	observed.client.outgoing <- destinationOf(t, "192.0.2.1", 53)

	eventuallyEqual(t, "outbound_cannot_carry_udp drops", func() uint64 { return inboundDrops(registry)["outbound_cannot_carry_udp"] }, 1)
	if got := registry.OutboundTraffic()["freedom"].DatagramDrops; got != 0 {
		t.Errorf("the outbound counted %d drops for a loss that is the inbound's", got)
	}
}

func TestAFailureTheSessionAlreadyCountedIsNotCountedAgainOnTheInbound(t *testing.T) {
	registry := newRelayStats()
	failing := &fakeOutbound{tag: "freedom", dialErr: errors.New("proxy refused")}
	observed := runCountingRelay(t, registry, func(spec.Destination) (component.Outbound, error) { return failing, nil })

	observed.client.outgoing <- destinationOf(t, "192.0.2.1", 53)

	eventuallyEqual(t, "outbound_open_failed drops", func() uint64 {
		return registry.OutboundTraffic()["freedom"].DatagramDropsByReason["outbound_open_failed"]
	}, 1)
	if got := registry.InboundTraffic()["in"].DatagramDrops; got != 0 {
		t.Errorf("the inbound counted %d drops for a loss the session counted on the outbound", got)
	}
}

func TestTheDestinationIsToldTheSizeOfEachAcceptedPacketOnly(t *testing.T) {
	registry := newRelayStats()
	working := &fakeOutbound{tag: "freedom"}
	failing := &fakeOutbound{tag: "failing", dialErr: errors.New("proxy refused")}
	accepted, rejected := destinationOf(t, "192.0.2.1", 53), destinationOf(t, "192.0.2.2", 53)
	observed := runCountingRelay(t, registry, func(destination spec.Destination) (component.Outbound, error) {
		if destination == rejected {
			return failing, nil
		}
		return working, nil
	})

	observed.client.outgoing <- rejected
	observed.client.outgoing <- accepted
	select {
	case size := <-observed.uplinks:
		if size != len("packet") {
			t.Errorf("uplink size = %d, want %d", size, len("packet"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an accepted packet was not reported")
	}
	select {
	case size := <-observed.uplinks:
		t.Errorf("a second packet was reported as accepted (size %d), but one failed to open", size)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestEachReplyIsReportedWithItsSenderAndSizeBeforeItIsWritten(t *testing.T) {
	registry := newRelayStats()
	out := &fakeOutbound{tag: "freedom"}
	observed := runCountingRelay(t, registry, func(spec.Destination) (component.Outbound, error) { return out, nil })
	target := destinationOf(t, "192.0.2.1", 53)

	observed.client.outgoing <- target
	waitForWrites(t, out, 1)
	out.onlyDatagram(t).incoming <- target

	select {
	case size := <-observed.replies:
		if size != len("reply") {
			t.Errorf("reply size = %d, want %d", size, len("reply"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a reply was not reported")
	}
}

func TestAReplyThatCannotBeWrittenToTheClientIsADropOnTheOutboundItCameFrom(t *testing.T) {
	registry := newRelayStats()
	out := &fakeOutbound{tag: "freedom"}
	observed := runCountingRelay(t, registry, func(spec.Destination) (component.Outbound, error) { return out, nil })
	target := destinationOf(t, "192.0.2.1", 53)
	tooLarge := spec.ErrMessageTooLong
	observed.client.writeFailure.Store(&tooLarge)

	observed.client.outgoing <- target
	waitForWrites(t, out, 1)
	out.onlyDatagram(t).incoming <- target

	eventuallyEqual(t, "reply_not_delivered drops", func() uint64 {
		return registry.OutboundTraffic()["freedom"].DatagramDropsByReason["reply_not_delivered"]
	}, 1)
	observed.udpSession.FlushCounts()
	if got := registry.OutboundTraffic()["freedom"].UDP.DownlinkPackets; got != 1 {
		t.Errorf("downlink packets = %d, want 1: a reply lost after it was received still counts as received", got)
	}
}

func TestTheRelayCountsNothingWithoutStats(t *testing.T) {
	out := &fakeOutbound{tag: "freedom"}
	router := &countingRouter{pick: func(spec.Destination) (component.Outbound, error) { return out, nil }}
	client := newClientDatagram()
	runLoop(t, client, router)

	client.readFailures <- spec.ErrMessageTooLong
	client.outgoing <- destinationOf(t, "192.0.2.1", 53)
	waitForWrites(t, out, 1)
}
