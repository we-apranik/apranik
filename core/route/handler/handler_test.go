package handler_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/module/transport/udp"
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

	dialer, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	freedom := outbound.NewFreedom(outbound.FreedomConfig{Tag: "freedom", DatagramDialer: dialer})
	trafficHandler := handler.New(router.New(freedom), handler.Config{})

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

	trafficHandler := handler.New(router.New(outbound.NewBlock("block")), handler.Config{})
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
