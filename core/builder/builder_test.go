package builder_test

import (
	"context"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/builder"
	proxyhttp "github.com/we-apranik/apranik/module/proxy/http"
	"github.com/we-apranik/apranik/module/proxy/socks5"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/spec"
)

// A counting no-op overlay proves the builder builds each layer's client and
// server side in stack order. Per-connection wrapping is exercised with the
// real TLS module in the end-to-end tests.
type countingOverlay struct {
	label   string
	applied *[]string
	mu      *sync.Mutex
}

func endpoint(ip string, port uint16) spec.Endpoint {
	e, err := spec.NewEndpoint(netip.MustParseAddr(ip), port)
	if err != nil {
		panic(err)
	}
	return e
}

func (countingOverlay) Validate() error    { return nil }
func (countingOverlay) Warnings() []string { return nil }

func (o countingOverlay) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: o.label, Description: "a counting no-op overlay"}
}

func (o countingOverlay) note() {
	o.mu.Lock()
	defer o.mu.Unlock()
	*o.applied = append(*o.applied, o.label)
}

func (o countingOverlay) NewStreamOverlayClient() (spec.StreamOverlayClient, error) {
	o.note()
	return passthroughOverlay{}, nil
}

func (o countingOverlay) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	o.note()
	return passthroughOverlay{}, nil
}

type passthroughOverlay struct{}

func (passthroughOverlay) WrapClientStream(_ context.Context, underlyingStream spec.Stream, _ spec.Destination) (spec.Stream, error) {
	return underlyingStream, nil
}

func (passthroughOverlay) WrapServerStream(_ context.Context, underlyingStream spec.Stream, _ *spec.TransportMetadata) (spec.Stream, error) {
	return underlyingStream, nil
}

func TestStreamTransportStackComposesOutwardFromTheWire(t *testing.T) {
	var (
		mu             sync.Mutex
		listenerLayers []string
		dialerLayers   []string
	)

	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	defer built.Close()

	listenerStack := blueprint.StreamTransportStack{
		Base: tcp.NewBaseStreamTransportModule(),
		Overlays: []spec.OverlayStreamTransportModule{
			countingOverlay{label: "inner", applied: &listenerLayers, mu: &mu},
			countingOverlay{label: "outer", applied: &listenerLayers, mu: &mu},
		},
	}
	dialerStack := blueprint.StreamTransportStack{
		Base: tcp.NewBaseStreamTransportModule(),
		Overlays: []spec.OverlayStreamTransportModule{
			countingOverlay{label: "inner", applied: &dialerLayers, mu: &mu},
			countingOverlay{label: "outer", applied: &dialerLayers, mu: &mu},
		},
	}

	inbound, err := built.BuildInbound(blueprint.Inbound{
		Tag:             "test-in",
		Bind:            endpoint("127.0.0.1", 0),
		Network:         spec.NetworkTCP,
		StreamTransport: listenerStack,
		StreamProxy:     passthroughProxy{},
	})
	if err != nil {
		t.Fatalf("BuildInbound: %v", err)
	}
	defer inbound.Close()

	outbound, err := built.BuildOutbound(blueprint.Outbound{Tag: "test-out", Freedom: &blueprint.Freedom{StreamTransport: dialerStack}})
	if err != nil {
		t.Fatalf("BuildOutbound: %v", err)
	}
	defer outbound.Close()

	// Base first, then each overlay outward from the wire.
	want := []string{"inner", "outer"}
	mu.Lock()
	defer mu.Unlock()
	if len(listenerLayers) != 2 || listenerLayers[0] != want[0] || listenerLayers[1] != want[1] {
		t.Errorf("listener layers applied %v, want %v", listenerLayers, want)
	}
	if len(dialerLayers) != 2 || dialerLayers[0] != want[0] || dialerLayers[1] != want[1] {
		t.Errorf("dialer layers applied %v, want %v", dialerLayers, want)
	}

	if networks := outbound.Networks(); len(networks) != 1 || networks[0] != spec.NetworkTCP {
		t.Errorf("Networks() = %v, want [tcp]", networks)
	}
}

// A failing overlay must not leak what was already built underneath it.
type failingOverlay struct{ countingOverlay }

func (failingOverlay) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestAFailedOverlayLeavesNoPortOpen(t *testing.T) {
	var (
		mu     sync.Mutex
		layers []string
	)

	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	defer built.Close()

	base := tcp.NewBaseStreamTransportModule()
	bind := endpoint("127.0.0.1", freeLoopbackPort(t))
	_, err = built.BuildInbound(blueprint.Inbound{
		Tag:     "doomed",
		Bind:    bind,
		Network: spec.NetworkTCP,
		StreamTransport: blueprint.StreamTransportStack{
			Base: base,
			Overlays: []spec.OverlayStreamTransportModule{
				failingOverlay{countingOverlay{label: "boom", applied: &layers, mu: &mu}},
			},
		},
		StreamProxy: passthroughProxy{},
	})
	if err == nil {
		t.Fatal("a failing overlay was accepted")
	}

	// The overlay fails before the listener binds, so the port is free.
	listener, rebindErr := base.NewStreamListener(bind)
	if rebindErr != nil {
		t.Fatalf("rebinding after a failed overlay: %v", rebindErr)
	}
	listener.Close()
}

// The smallest thing that satisfies the proxy kind: it consumes no preamble
// and reports a fixed destination.
type passthroughProxy struct{}

func (passthroughProxy) Validate() error    { return nil }
func (passthroughProxy) Warnings() []string { return nil }

func (passthroughProxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "passthrough", Description: "a test proxy"}
}

func (passthroughProxy) NewStreamServer() (spec.StreamProxyServer, error) {
	return passthroughServer{}, nil
}

func (passthroughProxy) NewStreamClient() (spec.StreamProxyClient, error) {
	return passthroughClient{}, nil
}

type passthroughServer struct{}

func (passthroughServer) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	return underlyingStream, spec.Metadata{Network: spec.NetworkTCP}, nil
}

type passthroughClient struct{}

func (passthroughClient) RequestStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	return underlyingStream, nil
}

type dispatchedNetworks chan spec.Network

func (d dispatchedNetworks) HandleStream(_ context.Context, c spec.Stream, _ spec.Metadata) error {
	d <- spec.NetworkTCP
	return c.Close()
}

func (d dispatchedNetworks) HandleDatagram(_ context.Context, c spec.Datagram, _ spec.Metadata) error {
	d <- spec.NetworkUDP
	return c.Close()
}

func freeLoopbackPort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

// The builder must hand the associate socket through the mixed server to its
// SOCKS5 half, or UDP ASSOCIATE is refused with X'07'.
func TestAMixedInboundServesSOCKS5UDPAndHTTP(t *testing.T) {
	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	defer built.Close()

	port := freeLoopbackPort(t)
	in, err := built.BuildInbound(blueprint.Inbound{
		Tag:             "mixed-in",
		Bind:            endpoint("127.0.0.1", port),
		Network:         spec.NetworkTCP,
		StreamTransport: blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()},
		Mixed:           &blueprint.Mixed{SOCKS5: socks5.NewProxyModule(), HTTP: proxyhttp.NewProxyModule()},
	})
	if err != nil {
		t.Fatalf("BuildInbound: %v", err)
	}
	handler := make(dispatchedNetworks, 1)
	if err := in.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	for _, tc := range []struct {
		name    string
		request []byte
		want    spec.Network
	}{
		{"socks5 udp associate", []byte{0x05, 0x01, 0x00, 0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}, spec.NetworkUDP},
		{"http connect", []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"), spec.NetworkTCP},
	} {
		connection, err := net.Dial("tcp", netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port).String())
		if err != nil {
			t.Fatal(err)
		}
		connection.Write(tc.request)

		select {
		case got := <-handler:
			if got != tc.want {
				t.Errorf("%s: dispatched as %s, want %s", tc.name, got, tc.want)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s: never reached the handler", tc.name)
		}
		connection.Close()
	}
}

// A blueprint written in Go skips config.Check, so the builder must refuse the
// pieces Check would have caught instead of dereferencing nil.
func TestAnIncompleteBlueprintIsAnErrorNotAPanic(t *testing.T) {
	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	defer built.Close()

	tcpStack := blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()}
	for name, inboundBlueprint := range map[string]blueprint.Inbound{
		"no protocol":       {Tag: "in", Bind: endpoint("127.0.0.1", 0), StreamTransport: tcpStack},
		"half of mixed":     {Tag: "in", Bind: endpoint("127.0.0.1", 0), StreamTransport: tcpStack, Mixed: &blueprint.Mixed{SOCKS5: socks5.NewProxyModule()}},
		"no base transport": {Tag: "in", Bind: endpoint("127.0.0.1", 0), StreamProxy: passthroughProxy{}},
	} {
		if _, err := built.BuildInbound(inboundBlueprint); err == nil {
			t.Errorf("inbound with %s was built", name)
		}
	}

	if _, err := built.BuildOutbound(blueprint.Outbound{Tag: "out", Server: destination(t), Proxy: passthroughProxy{}}); err == nil {
		t.Error("a proxy outbound with no transport stack was built")
	}
}

func destination(t *testing.T) spec.Destination {
	t.Helper()
	server, err := spec.NewDestinationFromDomain("example.com", 1080)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
