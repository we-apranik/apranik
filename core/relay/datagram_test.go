// Package relay_test drives the UDP relay loop with a fake client carrier and
// fake outbounds, so what was routed and what was sent are both observable.
package relay_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/net/udp/session"
	"github.com/The-Apranik/Apranik/core/relay"
	"github.com/The-Apranik/Apranik/spec"
)

const shortWait = 500 * time.Millisecond

func destinationOf(t *testing.T, host string, port uint16) spec.Destination {
	t.Helper()

	made, err := spec.NewDestinationFromIP(netip.MustParseAddr(host), port)
	if err != nil {
		t.Fatalf("NewDestinationFromIP: %v", err)
	}
	return made
}

// clientCarrier stands in for the inbound's side: tests push packets in and
// read back whatever the loop writes towards the client.
type clientCarrier struct {
	outgoing chan spec.Destination
	returned chan spec.Destination

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func newClientCarrier() *clientCarrier {
	return &clientCarrier{
		outgoing: make(chan spec.Destination, 16),
		returned: make(chan spec.Destination, 16),
		done:     make(chan struct{}),
	}
}

func (c *clientCarrier) ReadFrom(payload []byte) (int, spec.Destination, error) {
	select {
	case to := <-c.outgoing:
		return copy(payload, []byte("packet")), to, nil
	case <-c.done:
		return 0, spec.Destination{}, errors.New("client: closed")
	}
}

func (c *clientCarrier) WriteTo(payload []byte, from spec.Destination) (int, error) {
	select {
	case c.returned <- from:
		return len(payload), nil
	case <-c.done:
		return 0, errors.New("client: closed")
	}
}

func (c *clientCarrier) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	return nil
}

func (c *clientCarrier) LocalAddr() net.Addr { return nil }

type outboundCarrier struct {
	incoming chan spec.Destination
	done     chan struct{}

	mu      sync.Mutex
	written []spec.Destination
	closed  bool
}

func newOutboundCarrier() *outboundCarrier {
	return &outboundCarrier{incoming: make(chan spec.Destination, 8), done: make(chan struct{})}
}

func (o *outboundCarrier) WriteTo(payload []byte, to spec.Destination) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return 0, errors.New("outbound: closed")
	}
	o.written = append(o.written, to)
	return len(payload), nil
}

func (o *outboundCarrier) ReadFrom(payload []byte) (int, spec.Destination, error) {
	select {
	case from := <-o.incoming:
		return copy(payload, []byte("reply")), from, nil
	case <-o.done:
		return 0, spec.Destination{}, errors.New("outbound: closed")
	}
}

func (o *outboundCarrier) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.closed = true
		close(o.done)
	}
	return nil
}

func (o *outboundCarrier) LocalAddr() net.Addr { return nil }

func (o *outboundCarrier) writes() []spec.Destination {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]spec.Destination(nil), o.written...)
}

type fakeOutbound struct {
	tag string

	mu       sync.Mutex
	carriers []*outboundCarrier
}

func (o *fakeOutbound) Tag() string              { return o.tag }
func (o *fakeOutbound) Networks() []spec.Network { return []spec.Network{spec.NetworkUDP} }
func (o *fakeOutbound) Close() error             { return nil }

func (o *fakeOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return nil, spec.ErrUnsupportedNetwork
}

func (o *fakeOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	carrier := newOutboundCarrier()
	o.mu.Lock()
	o.carriers = append(o.carriers, carrier)
	o.mu.Unlock()
	return carrier, nil
}

func (o *fakeOutbound) onlyCarrier(t *testing.T) *outboundCarrier {
	t.Helper()

	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.carriers) != 1 {
		t.Fatalf("outbound %q opened %d carriers, want 1", o.tag, len(o.carriers))
	}
	return o.carriers[0]
}

type countingRouter struct {
	asked atomic.Int32
	pick  func(spec.Destination) (spec.Outbound, error)
}

func (r *countingRouter) outboundForDestination(_ context.Context, destination spec.Destination) (spec.Outbound, error) {
	r.asked.Add(1)
	return r.pick(destination)
}

func runLoop(t *testing.T, client *clientCarrier, router *countingRouter) *session.Session {
	t.Helper()

	udpSession := session.New(context.Background(), session.Config{})

	finished := make(chan struct{})
	go func() {
		relay.Datagrams(context.Background(), relay.DatagramRelayConfig{
			ClientSide:             client,
			ClientSession:          udpSession,
			OutboundForDestination: router.outboundForDestination,
		})
		close(finished)
	}()

	t.Cleanup(func() {
		client.Close()
		udpSession.Close()
		<-finished
	})
	return udpSession
}

func TestEveryPacketAsksForItsOutbound(t *testing.T) {
	out := &fakeOutbound{tag: "freedom"}
	router := &countingRouter{pick: func(spec.Destination) (spec.Outbound, error) { return out, nil }}

	client := newClientCarrier()
	runLoop(t, client, router)

	first := destinationOf(t, "192.0.2.1", 53)
	second := destinationOf(t, "192.0.2.2", 443)
	for range 5 {
		client.outgoing <- first
		client.outgoing <- second
	}

	waitForWrites(t, out, 10)

	if asked := int(router.asked.Load()); asked != 10 {
		t.Errorf("asked %d times for 10 packets, want 10", asked)
	}
}

func TestABlockedDestinationDropsOnlyThatPacket(t *testing.T) {
	out := &fakeOutbound{tag: "freedom"}
	blocked := destinationOf(t, "192.0.2.99", 9)
	allowed := destinationOf(t, "192.0.2.1", 53)
	refused := errors.New("blocked by rule")

	router := &countingRouter{pick: func(destination spec.Destination) (spec.Outbound, error) {
		if destination == blocked {
			return nil, refused
		}
		return out, nil
	}}

	client := newClientCarrier()
	udpSession := runLoop(t, client, router)

	client.outgoing <- blocked
	client.outgoing <- allowed

	waitForWrites(t, out, 1)

	if writes := out.onlyCarrier(t).writes(); len(writes) != 1 || writes[0] != allowed {
		t.Errorf("the outbound saw %v, want only %v", writes, allowed)
	}

	select {
	case <-udpSession.Done():
		t.Error("a blocked destination ended the session")
	default:
	}
}

func TestTwoDestinationsLeaveThroughTwoOutboundsAndBothRepliesReturn(t *testing.T) {
	dns := &fakeOutbound{tag: "dns-freedom"}
	game := &fakeOutbound{tag: "game-proxy"}

	dnsAddr := destinationOf(t, "192.0.2.1", 53)
	gameAddr := destinationOf(t, "198.51.100.7", 27015)

	router := &countingRouter{pick: func(destination spec.Destination) (spec.Outbound, error) {
		if destination == dnsAddr {
			return dns, nil
		}
		return game, nil
	}}

	client := newClientCarrier()
	runLoop(t, client, router)

	client.outgoing <- dnsAddr
	client.outgoing <- gameAddr

	waitForWrites(t, dns, 1)
	waitForWrites(t, game, 1)

	if writes := dns.onlyCarrier(t).writes(); len(writes) != 1 || writes[0] != dnsAddr {
		t.Errorf("dns outbound saw %v, want [%v]", writes, dnsAddr)
	}
	if writes := game.onlyCarrier(t).writes(); len(writes) != 1 || writes[0] != gameAddr {
		t.Errorf("game outbound saw %v, want [%v]", writes, gameAddr)
	}

	dns.onlyCarrier(t).incoming <- dnsAddr
	game.onlyCarrier(t).incoming <- gameAddr

	seen := map[spec.Destination]bool{}
	for range 2 {
		select {
		case from := <-client.returned:
			seen[from] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 2 replies reached the client", len(seen))
		}
	}
	if !seen[dnsAddr] || !seen[gameAddr] {
		t.Errorf("client saw %v, want both %v and %v", seen, dnsAddr, gameAddr)
	}
}

func waitForWrites(t *testing.T, out *fakeOutbound, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		out.mu.Lock()
		total := 0
		for _, carrier := range out.carriers {
			total += len(carrier.writes())
		}
		out.mu.Unlock()
		if total >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("outbound %q did not receive %d packets in time", out.tag, want)
}
