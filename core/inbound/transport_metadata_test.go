package inbound_test

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/inbound"
	"github.com/we-apranik/apranik/module/proxy/trojan"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/spec"
)

// sourceReplacingOverlay stands in for an overlay that learned the real
// client from a trusted proxy. It also overwrites the socket's ends, which core
// must not keep.
type sourceReplacingOverlay struct {
	replacement  spec.Endpoint
	sourceBefore *spec.Endpoint
}

func (o sourceReplacingOverlay) WrapServerStream(_ context.Context, underlyingStream spec.Stream, transport *spec.TransportMetadata) (spec.Stream, error) {
	if o.sourceBefore != nil {
		*o.sourceBefore = transport.SourceEndpoint
	}
	transport.SourceEndpoint = o.replacement
	transport.InboundRemoteEndpoint = o.replacement
	transport.InboundLocalEndpoint = o.replacement
	return underlyingStream, nil
}

func startTrojanInboundWithOverlays(t *testing.T, overlays ...inbound.StreamOverlay) (net.Addr, recordingHandler) {
	t.Helper()
	listener, err := tcp.NewBaseStreamTransportModule().NewStreamListener(loopback(t))
	if err != nil {
		t.Fatal(err)
	}
	proxy := &trojan.Proxy{Settings: trojan.Settings{Users: []trojan.User{{Name: "alice", Password: "secret"}}}}
	streamServer, err := proxy.NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	in := inbound.NewStream(inbound.StreamConfig{Tag: "trojan-in", Listener: listener, Overlays: overlays, ProxyServer: streamServer})
	handler := recordingHandler{calls: make(chan handled, 1)}
	if err := in.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return listener.LocalAddr(), handler
}

func connectAndReadWhatTheHandlerGot(t *testing.T, address net.Addr, handler recordingHandler) (clientSide net.Addr, received spec.Metadata) {
	t.Helper()
	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write(request(t, 1))
	select {
	case got := <-handler.calls:
		return connection.LocalAddr(), got.metadata
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the handler")
		return nil, spec.Metadata{}
	}
}

func TestSourceIsTheSocketsRemoteEndWhenNoOverlayReplacesIt(t *testing.T) {
	address, handler := startTrojanInboundWithOverlays(t)

	clientSide, got := connectAndReadWhatTheHandlerGot(t, address, handler)

	want := spec.NewEndpointFromNetAddr(clientSide)
	if got.Transport.SourceEndpoint != want || got.Transport.InboundRemoteEndpoint != want {
		t.Errorf("Source = %s, InboundRemoteEndpoint = %s; both want the socket's remote end %s",
			got.Transport.SourceEndpoint, got.Transport.InboundRemoteEndpoint, want)
	}
}

func TestAnOverlayReplacesSourceAndNothingElseOfTheSocket(t *testing.T) {
	address, handler := startTrojanInboundWithOverlays(t, inbound.StreamOverlay{
		Name:   "replacing",
		Server: sourceReplacingOverlay{replacement: spec.NewEndpointFromNetAddr(&net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 4321})},
	})

	clientSide, got := connectAndReadWhatTheHandlerGot(t, address, handler)

	if want := netip.MustParseAddr("203.0.113.9"); got.Transport.SourceEndpoint.Addr() != want {
		t.Errorf("Source = %s, want the address the overlay reported, %s", got.Transport.SourceEndpoint, want)
	}
	if want := spec.NewEndpointFromNetAddr(clientSide); got.Transport.InboundRemoteEndpoint != want {
		t.Errorf("InboundRemoteEndpoint = %s, want the socket's remote end %s: an overlay may not change it", got.Transport.InboundRemoteEndpoint, want)
	}
	if want := spec.NewEndpointFromNetAddr(address); got.Transport.InboundLocalEndpoint != want {
		t.Errorf("InboundLocalEndpoint = %s, want the address the client reached, %s: an overlay may not change it", got.Transport.InboundLocalEndpoint, want)
	}
}

func TestAnOverlaySeesWhatTheOverlayBelowItReported(t *testing.T) {
	var seenByUpperOverlay spec.Endpoint
	lowerReported := spec.NewEndpointFromNetAddr(&net.TCPAddr{IP: net.ParseIP("198.51.100.7"), Port: 1})
	address, handler := startTrojanInboundWithOverlays(t,
		inbound.StreamOverlay{Name: "lower", Server: sourceReplacingOverlay{replacement: lowerReported}},
		inbound.StreamOverlay{Name: "upper", Server: sourceReplacingOverlay{
			replacement:  spec.NewEndpointFromNetAddr(&net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 4321}),
			sourceBefore: &seenByUpperOverlay,
		}},
	)

	_, got := connectAndReadWhatTheHandlerGot(t, address, handler)

	if seenByUpperOverlay != lowerReported {
		t.Errorf("the upper overlay saw Source %s, want what the lower one reported, %s", seenByUpperOverlay, lowerReported)
	}
	if want := netip.MustParseAddr("203.0.113.9"); got.Transport.SourceEndpoint.Addr() != want {
		t.Errorf("Source = %s, want the last overlay's, %s", got.Transport.SourceEndpoint, want)
	}
}
