package session_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/core/net/udp/session"
	"github.com/we-apranik/apranik/core/outbound"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/module/transport/base/udp"
	"github.com/we-apranik/apranik/spec"
)

func newStatsRegistry() *stats.Registry {
	return stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"},
		Outbounds:   []stats.OutboundDescription{{Tag: "out"}, {Tag: "member"}, {Tag: "auto", IsGroup: true}},
	})
}

func statsSessionConfig(registry *stats.Registry) session.Config {
	return session.Config{Metadata: spec.Metadata{Network: spec.NetworkUDP, InboundTag: "in", User: "alice"}, Stats: registry}
}

type carriedFakeDatagram struct {
	*fakeDatagram
	tags []string
}

func (d carriedFakeDatagram) CarryingOutboundTags() []string { return d.tags }

type fakeGroup struct {
	*fakeOutbound
	memberTag string
}

func (g fakeGroup) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	datagram, err := g.fakeOutbound.CreateDatagram(ctx, metadata)
	if err != nil {
		return nil, err
	}
	return carriedFakeDatagram{fakeDatagram: datagram.(*fakeDatagram), tags: []string{g.memberTag}}, nil
}

func TestSentAndReceivedDatagramsAreCountedOnTheClientAndTheOutbound(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	udpSession := newSession(t, statsSessionConfig(registry))
	target := destinationOf(t, "192.0.2.1", 53)

	if err := udpSession.Send(make([]byte, 10), target, out); err != nil {
		t.Fatal(err)
	}
	if err := udpSession.Send(make([]byte, 20), target, out); err != nil {
		t.Fatal(err)
	}
	out.datagramAt(t, 0).incoming <- target
	expectReply(t, udpSession, shortWait).Release()
	udpSession.FlushCounts()

	want := stats.UDPTraffic{UplinkBytes: 30, UplinkPackets: 2, DownlinkBytes: 5, DownlinkPackets: 1, Sessions: 1}
	for name, got := range map[string]stats.Traffic{
		"inbound":  registry.InboundTraffic()["in"].Traffic,
		"user":     registry.UserTraffic()["alice"],
		"outbound": registry.OutboundTraffic()["out"].Traffic,
	} {
		if got.UDP != want {
			t.Errorf("%s UDP = %+v, want %+v", name, got.UDP, want)
		}
		if got.TCP != (stats.TCPTraffic{}) {
			t.Errorf("%s TCP = %+v, want none: UDP datagrams are not TCP", name, got.TCP)
		}
	}
}

func TestAReplyNamesTheOutboundItCameFrom(t *testing.T) {
	out := newFakeOutbound("out")
	udpSession := newSession(t, session.Config{})
	target := destinationOf(t, "192.0.2.1", 53)
	if err := udpSession.Send([]byte("x"), target, out); err != nil {
		t.Fatal(err)
	}
	out.datagramAt(t, 0).incoming <- target

	reply := expectReply(t, udpSession, shortWait)
	defer reply.Release()
	if reply.OutboundTag != "out" {
		t.Errorf("reply came from outbound %q, want out", reply.OutboundTag)
	}
}

func TestAGroupsDatagramIsCountedUnderTheGroupAndTheMember(t *testing.T) {
	registry := newStatsRegistry()
	group := fakeGroup{fakeOutbound: newFakeOutbound("auto"), memberTag: "member"}
	udpSession := newSession(t, statsSessionConfig(registry))
	target := destinationOf(t, "192.0.2.1", 53)

	if err := udpSession.Send(make([]byte, 40), target, group); err != nil {
		t.Fatal(err)
	}
	udpSession.FlushCounts()

	outbounds := registry.OutboundTraffic()
	for _, tag := range []string{"auto", "member"} {
		if got := outbounds[tag].UDP; got.UplinkBytes != 40 || got.UplinkPackets != 1 || got.Sessions != 1 {
			t.Errorf("%s UDP = %+v, want 40 bytes, 1 packet, 1 session", tag, got)
		}
	}
	if got := outbounds["out"].UDP; got != (stats.UDPTraffic{}) {
		t.Errorf("an uninvolved outbound counted %+v", got)
	}
	if tags := udpSession.CarryingOutboundTags(group); !slices.Equal(tags, []string{"auto", "member"}) {
		t.Errorf("CarryingOutboundTags = %v, want [auto member]", tags)
	}
}

func TestACarryingOutboundTagListExistsOnlyAfterTheOutboundCarriedADatagram(t *testing.T) {
	out := newFakeOutbound("out")
	udpSession := newSession(t, session.Config{})
	if tags := udpSession.CarryingOutboundTags(out); tags != nil {
		t.Errorf("tags before any send = %v", tags)
	}
	if err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), out); err != nil {
		t.Fatal(err)
	}
	if tags := udpSession.CarryingOutboundTags(out); !slices.Equal(tags, []string{"out"}) {
		t.Errorf("tags = %v, want [out]", tags)
	}
}

func TestAFailedSendCountsADropOnTheOutboundAndNoBytes(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	udpSession := newSession(t, statsSessionConfig(registry))
	target := destinationOf(t, "192.0.2.1", 53)
	if err := udpSession.Send([]byte("open"), target, out); err != nil {
		t.Fatal(err)
	}
	out.datagramAt(t, 0).failWrites(errors.New("network is down"))

	err := udpSession.Send(make([]byte, 100), target, out)
	udpSession.FlushCounts()
	if !errors.Is(err, session.ErrSendingToOutbound) {
		t.Fatalf("Send error = %v, want one wrapping ErrSendingToOutbound so the relay does not count it again", err)
	}

	got := registry.OutboundTraffic()["out"]
	if got.DatagramDropsByReason["send_failed"] != 1 || got.DatagramDrops != 1 {
		t.Errorf("outbound drops = %v, want one send_failed", got.DatagramDropsByReason)
	}
	if got.UDP.UplinkBytes != 4 || got.UDP.UplinkPackets != 1 {
		t.Errorf("outbound UDP = %+v, want only the first packet counted", got.UDP)
	}
	if inbound := registry.InboundTraffic()["in"]; inbound.DatagramDrops != 0 {
		t.Errorf("the inbound counted %d drops for a loss on the outbound's side", inbound.DatagramDrops)
	}
}

func TestAFailedOpenCountsADialFailureAndADropOnTheOutbound(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	out.dialErr = errors.New("proxy server refused")
	config := statsSessionConfig(registry)
	config.DialFailureReasonFor = func(error) stats.DialFailureReason { return stats.DialRefused }
	udpSession := newSession(t, config)

	err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), out)
	if !errors.Is(err, session.ErrOpeningOutbound) || !errors.Is(err, out.dialErr) {
		t.Fatalf("Send error = %v, want ErrOpeningOutbound wrapping the cause", err)
	}

	got := registry.OutboundTraffic()["out"]
	if got.DialFailuresByReason["refused"] != 1 || got.DatagramDropsByReason["outbound_open_failed"] != 1 {
		t.Errorf("dial failures %v, drops %v, want one refused and one outbound_open_failed", got.DialFailuresByReason, got.DatagramDropsByReason)
	}
	if got.UDP.Sessions != 0 {
		t.Errorf("an outbound that never opened counted %d sessions", got.UDP.Sessions)
	}
}

func TestAWriteThatFailsToOpenTheWayToADestinationCountsADialFailure(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	config := statsSessionConfig(registry)
	config.DialFailureReasonFor = func(error) stats.DialFailureReason { return stats.DialRefused }
	udpSession := newSession(t, config)

	if err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), out); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("proxy server refused")
	out.datagramAt(t, 0).failWrites(fmt.Errorf("%w: %w", component.ErrOpeningDatagramForDestination, cause))

	err := udpSession.Send([]byte("y"), destinationOf(t, "192.0.2.2", 53), out)
	if !errors.Is(err, session.ErrOpeningOutbound) || !errors.Is(err, cause) {
		t.Fatalf("Send error = %v, want ErrOpeningOutbound wrapping the cause", err)
	}

	got := registry.OutboundTraffic()["out"]
	if got.DialFailuresByReason["refused"] != 1 || got.DatagramDropsByReason["outbound_open_failed"] != 1 || got.DatagramDropsByReason["send_failed"] != 0 {
		t.Errorf("dial failures %v, drops %v, want one refused and one outbound_open_failed, no send_failed", got.DialFailuresByReason, got.DatagramDropsByReason)
	}
}

func TestAFailedOpenWithoutAClassifierIsADialFailureForAnyReason(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	out.dialErr = errors.New("boom")
	udpSession := newSession(t, statsSessionConfig(registry))

	udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), out)

	if got := registry.OutboundTraffic()["out"].DialFailuresByReason["other"]; got != 1 {
		t.Errorf("dial failures for other = %d, want 1", got)
	}
}

func TestAnOutboundWithoutUDPIsNotCountedByTheSession(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	out.networks = []spec.Network{spec.NetworkTCP}
	udpSession := newSession(t, statsSessionConfig(registry))

	err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), out)
	if !errors.Is(err, spec.ErrUnsupportedNetwork) || errors.Is(err, session.ErrOpeningOutbound) {
		t.Fatalf("Send error = %v, want ErrUnsupportedNetwork alone: the relay counts it on the inbound", err)
	}
	if got := registry.OutboundTraffic()["out"]; got.DatagramDrops != 0 || got.DialFailures != 0 {
		t.Errorf("the outbound counted %+v for a loss that is the inbound's", got)
	}
}

func TestAReplyTooLargeForTheBufferIsADropOnTheOutbound(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	udpSession := newSession(t, statsSessionConfig(registry))
	target := destinationOf(t, "192.0.2.1", 53)
	if err := udpSession.Send([]byte("x"), target, out); err != nil {
		t.Fatal(err)
	}

	out.datagramAt(t, 0).failNextRead(spec.ErrMessageTooLong)

	deadline := time.Now().Add(shortWait * 4)
	for time.Now().Before(deadline) {
		if registry.OutboundTraffic()["out"].DatagramDropsByReason["too_large_from_outbound"] == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("drops = %v, want one too_large_from_outbound", registry.OutboundTraffic()["out"].DatagramDropsByReason)
}

func TestAReplyTheSessionEndsBeforeHandingOnIsADropOnTheOutbound(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	udpSession := session.New(context.Background(), statsSessionConfig(registry))
	target := destinationOf(t, "192.0.2.1", 53)
	if err := udpSession.Send([]byte("x"), target, out); err != nil {
		t.Fatal(err)
	}

	out.datagramAt(t, 0).incoming <- target
	eventuallyDownlinkPackets(t, registry, udpSession, 1)
	udpSession.Close()

	deadline := time.Now().Add(shortWait * 4)
	for time.Now().Before(deadline) {
		if registry.OutboundTraffic()["out"].DatagramDropsByReason["reply_not_delivered"] == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("drops = %v, want one reply_not_delivered", registry.OutboundTraffic()["out"].DatagramDropsByReason)
}

func eventuallyDownlinkPackets(t *testing.T, registry *stats.Registry, udpSession *session.Session, want uint64) {
	t.Helper()
	deadline := time.Now().Add(shortWait * 4)
	for time.Now().Before(deadline) {
		udpSession.FlushCounts()
		if registry.OutboundTraffic()["out"].UDP.DownlinkPackets == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("downlink packets = %d, want %d", registry.OutboundTraffic()["out"].UDP.DownlinkPackets, want)
}

func TestTwoOutboundsEachCountTheirOwnSession(t *testing.T) {
	registry := newStatsRegistry()
	first, second := newFakeOutbound("out"), newFakeOutbound("member")
	udpSession := newSession(t, statsSessionConfig(registry))
	target := destinationOf(t, "192.0.2.1", 53)

	for range 3 {
		udpSession.Send([]byte("x"), target, first)
	}
	udpSession.Send([]byte("x"), target, second)

	if got := registry.InboundTraffic()["in"].UDP.Sessions; got != 1 {
		t.Errorf("inbound sessions = %d, want 1: one client session", got)
	}
	for tag, want := range map[string]uint64{"out": 1, "member": 1} {
		if got := registry.OutboundTraffic()[tag].UDP.Sessions; got != want {
			t.Errorf("%s sessions = %d, want %d: once per session however many datagrams", tag, got, want)
		}
	}
}

var _ component.CarryingOutboundsReporter = carriedFakeDatagram{}

func TestRepliesFreedomsNATFiltersAreCountedAsDropsOnTheOutbound(t *testing.T) {
	registry := stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"},
		Outbounds:   []stats.OutboundDescription{{Tag: "freedom"}},
	})
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	sprayer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2)})
	if err != nil {
		t.Skipf("no second loopback address to spray from: %v", err)
	}
	defer sprayer.Close()

	dialer, err := (&udp.Transport{}).NewDatagramDialer(dns.NewSystemResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	freedom := outbound.NewFreedom(outbound.FreedomConfig{Tag: "freedom", DatagramDialer: dialer, Counters: registry.Outbound("freedom")})
	udpSession := newSession(t, statsSessionConfig(registry))
	target := destinationOf(t, "127.0.0.1", uint16(peer.LocalAddr().(*net.UDPAddr).Port))
	if err := udpSession.Send([]byte("hello"), target, freedom); err != nil {
		t.Fatal(err)
	}
	natAddress := readSource(t, peer)

	const sprayed = 7
	for range sprayed {
		if _, err := sprayer.WriteToUDP([]byte("spray"), natAddress); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if registry.OutboundTraffic()["freedom"].DatagramDropsByReason["rejected_by_nat"] == sprayed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := registry.OutboundTraffic()["freedom"]
	if got.DatagramDropsByReason["rejected_by_nat"] != sprayed {
		t.Errorf("drops = %v, want %d rejected_by_nat", got.DatagramDropsByReason, sprayed)
	}
	if got.UDP.DownlinkPackets != 0 {
		t.Errorf("downlink packets = %d: a filtered reply must not count as received", got.UDP.DownlinkPackets)
	}
}

func TestDatagramsAreAddedToTheSharedCountersInBatchesAndWhenTheSessionEnds(t *testing.T) {
	registry := newStatsRegistry()
	out := newFakeOutbound("out")
	udpSession := session.New(context.Background(), statsSessionConfig(registry))
	target := destinationOf(t, "192.0.2.1", 53)

	for range 10 {
		if err := udpSession.Send(make([]byte, 100), target, out); err != nil {
			t.Fatal(err)
		}
	}
	if got := registry.InboundTraffic()["in"].UDP.UplinkPackets; got != 0 {
		t.Errorf("shared uplink packets = %d after 10 datagrams, want 0: they are counted privately until a batch is full or a flush", got)
	}

	for range 60 {
		if err := udpSession.Send(make([]byte, 100), target, out); err != nil {
			t.Fatal(err)
		}
	}
	if got := registry.InboundTraffic()["in"].UDP.UplinkPackets; got < 64 {
		t.Errorf("shared uplink packets = %d after 70 datagrams, want a batch of 64 or more added", got)
	}

	udpSession.Close()
	got := registry.InboundTraffic()["in"].UDP
	if got.UplinkPackets != 70 || got.UplinkBytes != 7000 {
		t.Errorf("after the session ended: %d packets, %d bytes, want 70 and 7000: nothing may stay pending", got.UplinkPackets, got.UplinkBytes)
	}
	if outbound := registry.OutboundTraffic()["out"].UDP; outbound.UplinkPackets != 70 {
		t.Errorf("outbound uplink packets = %d after the session ended, want 70", outbound.UplinkPackets)
	}
}

func TestEveryCountedDatagramIsAddedWhateverTheConcurrency(t *testing.T) {
	registry := newStatsRegistry()
	const sessions, perSession = 8, 500
	target := destinationOf(t, "192.0.2.1", 53)

	var running sync.WaitGroup
	for range sessions {
		running.Add(1)
		go func() {
			defer running.Done()
			out := newFakeOutbound("out")
			udpSession := session.New(context.Background(), statsSessionConfig(registry))
			for range perSession {
				udpSession.Send(make([]byte, 10), target, out)
			}
			udpSession.Close()
		}()
	}
	running.Wait()

	got := registry.InboundTraffic()["in"].UDP
	if got.UplinkPackets != sessions*perSession || got.UplinkBytes != sessions*perSession*10 {
		t.Errorf("%d packets, %d bytes, want %d and %d", got.UplinkPackets, got.UplinkBytes, sessions*perSession, sessions*perSession*10)
	}
}
