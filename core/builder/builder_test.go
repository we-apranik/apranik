package builder_test

import (
	"context"
	"io"
	"net/netip"
	"sync"
	"testing"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/module/transport/tcp"
	"github.com/The-Apranik/Apranik/spec"
)

// This build ships no overlay transport, so the fold that composes a stack has
// no real implementation to exercise it. A counting no-op overlay proves the
// composition works and that each layer is handed what is already below it --
// the property the whole extension point rests on.
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

func (o countingOverlay) WrapStreamDialer(inner spec.StreamDialer) (spec.StreamDialer, error) {
	o.note()
	return inner, nil
}

func (o countingOverlay) WrapStreamListener(inner spec.StreamListener) (spec.StreamListener, error) {
	o.note()
	return inner, nil
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
		Overlays: []spec.OverlayStreamTransport{
			countingOverlay{label: "inner", applied: &listenerLayers, mu: &mu},
			countingOverlay{label: "outer", applied: &listenerLayers, mu: &mu},
		},
	}
	dialerStack := blueprint.StreamTransportStack{
		Base: tcp.NewBaseStreamTransportModule(),
		Overlays: []spec.OverlayStreamTransport{
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

func (failingOverlay) WrapStreamListener(spec.StreamListener) (spec.StreamListener, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestAFailedWrapClosesWhatWasBuilt(t *testing.T) {
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
	_, err = built.BuildInbound(blueprint.Inbound{
		Tag:     "doomed",
		Bind:    endpoint("127.0.0.1", 0),
		Network: spec.NetworkTCP,
		StreamTransport: blueprint.StreamTransportStack{
			Base: base,
			Overlays: []spec.OverlayStreamTransport{
				failingOverlay{countingOverlay{label: "boom", applied: &layers, mu: &mu}},
			},
		},
		StreamProxy: passthroughProxy{},
	})
	if err == nil {
		t.Fatal("a failing overlay was accepted")
	}

	// The listener beneath it bound a port; if it was not closed, binding the
	// same stack again on the same address would be the only way to notice.
	listener, rebindErr := base.NewStreamListener(endpoint("127.0.0.1", 0))
	if rebindErr != nil {
		t.Fatalf("rebinding after a failed wrap: %v", rebindErr)
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

func (passthroughServer) AcceptStream(ctx context.Context, carrier spec.Stream) (spec.Stream, spec.Metadata, error) {
	return carrier, spec.Metadata{Network: spec.NetworkTCP}, nil
}

type passthroughClient struct{}

func (passthroughClient) ConnectStream(ctx context.Context, carrier spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	return carrier, nil
}
