package handler_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/dns"
	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	httpsniffer "github.com/The-Apranik/Apranik/module/sniffer/http"
	"github.com/The-Apranik/Apranik/module/transport/base/udp"
	"github.com/The-Apranik/Apranik/spec"
)

// clientDatagram stands in for the datagram an inbound hands the handler for
// one UDP session, such as a SOCKS5 association.
type clientDatagram struct {
	outgoing chan spec.Destination
	returned chan []byte

	mu     sync.Mutex
	closed bool
	done   chan struct{}
	label  []byte
}

func newClientDatagram(label string) *clientDatagram {
	return &clientDatagram{
		outgoing: make(chan spec.Destination, 8),
		returned: make(chan []byte, 8),
		done:     make(chan struct{}),
		label:    []byte(label),
	}
}

func (c *clientDatagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	select {
	case to := <-c.outgoing:
		return copy(payload, c.label), to, nil
	case <-c.done:
		return 0, spec.Destination{}, errors.New("clientSide: closed")
	}
}

func (c *clientDatagram) WriteTo(payload []byte, _ spec.Destination) (int, error) {
	echoed := make([]byte, len(payload))
	copy(echoed, payload)
	select {
	case c.returned <- echoed:
		return len(payload), nil
	case <-c.done:
		return 0, errors.New("clientSide: closed")
	}
}

func (c *clientDatagram) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
	return nil
}

func (c *clientDatagram) LocalAddr() net.Addr { return nil }

// Two UDP sessions to the same destination must not see each other's
// replies: each has its own NAT and its own socket.
func TestTwoUDPSessionsToOneDestinationDoNotCross(t *testing.T) {
	echoServer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer echoServer.Close()

	// The echoServer echoes whatever it receives back to whoever sent it, so a
	// crossed reply is visible as the wrong label.
	go func() {
		buffer := make([]byte, 1500)
		for {
			size, from, err := echoServer.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			echoServer.WriteToUDP(buffer[:size], from)
		}
	}()

	local := echoServer.LocalAddr().(*net.UDPAddr)
	target, err := spec.NewDestinationFromIP(netip.MustParseAddr("127.0.0.1"), uint16(local.Port))
	if err != nil {
		t.Fatalf("NewDestinationFromIP: %v", err)
	}

	dialer, err := (&udp.Transport{}).NewDatagramDialer(dns.NewSystemResolver(nil))
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	freedom := outbound.NewFreedom(outbound.FreedomConfig{Tag: "freedom", DatagramDialer: dialer})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(freedom), handler.Config{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := newClientDatagram("first-session")
	second := newClientDatagram("second-session")
	for _, clientSide := range []*clientDatagram{first, second} {
		go trafficHandler.HandleDatagram(ctx, clientSide, spec.Metadata{Network: spec.NetworkUDP, InboundTag: "test"})
		defer clientSide.Close()
	}

	first.outgoing <- target
	second.outgoing <- target

	for _, clientSide := range []*clientDatagram{first, second} {
		select {
		case echoed := <-clientSide.returned:
			if string(echoed) != string(clientSide.label) {
				t.Errorf("session %q received %q: a reply crossed between sessions", clientSide.label, echoed)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("session %q never received its reply", clientSide.label)
		}
	}

	// Neither session may receive a second packet: one reply each, not both.
	for _, clientSide := range []*clientDatagram{first, second} {
		select {
		case extra := <-clientSide.returned:
			t.Errorf("session %q received a second packet %q", clientSide.label, extra)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func TestABlockedStreamIsClosedQuietly(t *testing.T) {
	clientSide, inboundSide := net.Pipe()
	defer clientSide.Close()

	trafficHandler := handler.New(blockEverything(t), handler.Config{})
	err := trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, spec.Metadata{Network: spec.NetworkTCP, InboundTag: "test"})
	if err != nil {
		t.Errorf("HandleStream returned %v, want nil: blocking is not a failure", err)
	}

	clientSide.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := clientSide.Read(make([]byte, 1)); err == nil {
		t.Error("the client's stream was left open")
	}
}

type blockTestStream struct{ net.Conn }

func (blockTestStream) CloseWrite() error { return spec.ErrNoHalfClose }

func blockEverything(t *testing.T) *router.Router {
	t.Helper()
	blocking, err := router.New(router.Config{Routing: blueprint.Routing{DefaultAction: blueprint.Action{Block: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return blocking
}

// Hands each dialed stream to the test, so it can see what the handler relayed.
type capturingOutbound struct {
	tag    string
	dialed chan net.Conn
}

func (o capturingOutbound) Tag() string              { return o.tag }
func (o capturingOutbound) Networks() []spec.Network { return []spec.Network{spec.NetworkTCP} }
func (o capturingOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	handlerSide, testSide := net.Pipe()
	o.dialed <- testSide
	return blockTestStream{handlerSide}, nil
}
func (o capturingOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return nil, spec.ErrUnsupportedNetwork
}
func (o capturingOutbound) Close() error { return nil }

// A rule sniffs, the next routes by the sniffed name, and the outbound still
// receives every byte the client sent, including the ones peeked.
func TestSniffedStreamIsRoutedByNameAndRelayedWhole(t *testing.T) {
	direct := capturingOutbound{tag: "direct", dialed: make(chan net.Conn, 1)}
	proxy := capturingOutbound{tag: "proxy", dialed: make(chan net.Conn, 1)}
	sniffingRouter, err := router.New(router.Config{
		Routing: blueprint.Routing{
			Rules: []blueprint.Rule{
				{Action: blueprint.Action{Sniff: &blueprint.SniffAction{Sniffers: []spec.SnifferModule{httpsniffer.NewSnifferModule()}}}},
				{Match: blueprint.Match{DomainSuffixes: []string{"ir"}}, Action: blueprint.Action{Route: &blueprint.RouteAction{Outbound: "direct"}}},
			},
			DefaultAction: blueprint.Action{Route: &blueprint.RouteAction{Outbound: "proxy"}},
		},
		Outbounds: map[string]component.Outbound{"direct": direct, "proxy": proxy},
	})
	if err != nil {
		t.Fatal(err)
	}

	clientSide, inboundSide := net.Pipe()
	defer clientSide.Close()
	destination, _ := spec.NewDestinationFromIP(netip.MustParseAddr("185.143.233.2"), 80)
	trafficHandler := handler.New(sniffingRouter, handler.Config{})
	go trafficHandler.HandleStream(context.Background(), deadlineStream{inboundSide}, spec.Metadata{Network: spec.NetworkTCP, Destination: destination, InboundTag: "test"})

	request := "GET / HTTP/1.1\r\nHost: digikala.ir\r\n\r\n"
	go clientSide.Write([]byte(request))

	select {
	case dialed := <-direct.dialed:
		defer dialed.Close()
		received := make([]byte, len(request))
		dialed.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.ReadFull(dialed, received); err != nil {
			t.Fatalf("reading what was relayed: %v", err)
		}
		if string(received) != request {
			t.Fatalf("relayed %q, want %q", received, request)
		}
	case <-proxy.dialed:
		t.Fatal("routed to proxy: the sniffed name was not used")
	case <-time.After(3 * time.Second):
		t.Fatal("nothing was dialed")
	}
}

// net.Pipe takes deadlines, which sniffing needs to bound its wait.
type deadlineStream struct{ net.Conn }

func (deadlineStream) CloseWrite() error { return spec.ErrNoHalfClose }

func TestBlockedUDPDestinationIsDroppedOthersPass(t *testing.T) {
	allowed := startUDPEchoServer(t)
	blocked := startUDPEchoServer(t)

	dialer, err := (&udp.Transport{}).NewDatagramDialer(dns.NewSystemResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	freedom := outbound.NewFreedom(outbound.FreedomConfig{Tag: "freedom", DatagramDialer: dialer})
	blockingRouter, err := router.New(router.Config{
		Routing: blueprint.Routing{
			Rules: []blueprint.Rule{{
				Match:  blueprint.Match{Ports: []blueprint.PortRange{{First: blocked.Port(), Last: blocked.Port()}}},
				Action: blueprint.Action{Block: true},
			}},
			DefaultAction: blueprint.Action{Route: &blueprint.RouteAction{Outbound: "freedom"}},
		},
		Outbounds: map[string]component.Outbound{"freedom": freedom},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientSide := newClientDatagram("payload")
	defer clientSide.Close()
	go handler.New(blockingRouter, handler.Config{}).HandleDatagram(ctx, clientSide, spec.Metadata{Network: spec.NetworkUDP, InboundTag: "test"})

	clientSide.outgoing <- blocked
	clientSide.outgoing <- blocked
	clientSide.outgoing <- allowed
	select {
	case <-clientSide.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("the allowed destination got no reply")
	}
	select {
	case extra := <-clientSide.returned:
		t.Fatalf("a second reply %q arrived: the blocked destination was reached", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

func startUDPEchoServer(t *testing.T) spec.Destination {
	t.Helper()
	echoServer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echoServer.Close() })
	go func() {
		buffer := make([]byte, 1500)
		for {
			size, from, err := echoServer.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			echoServer.WriteToUDP(buffer[:size], from)
		}
	}()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr("127.0.0.1"), uint16(echoServer.LocalAddr().(*net.UDPAddr).Port))
	if err != nil {
		t.Fatal(err)
	}
	return destination
}
