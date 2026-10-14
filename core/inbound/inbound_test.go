package inbound_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/core/inbound"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/module/proxy/trojan"
	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/spec"
)

type handled struct {
	network  spec.Network
	metadata spec.Metadata
}

// recordingHandler reports what core dispatched and holds nothing open.
type recordingHandler struct{ calls chan handled }

func (h recordingHandler) HandleStream(_ context.Context, c spec.Stream, m spec.Metadata) error {
	h.calls <- handled{spec.NetworkTCP, m}
	return c.Close()
}

func (h recordingHandler) HandleDatagram(_ context.Context, c spec.Datagram, m spec.Metadata) error {
	h.calls <- handled{spec.NetworkUDP, m}
	return c.Close()
}

func loopback(t *testing.T) spec.Endpoint {
	endpoint, err := spec.NewEndpoint(netip.MustParseAddr("127.0.0.1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

// startTrojanInbound runs a real inbound on loopback TCP, with the trojan
// module registered for both of its kinds, as the builder does.
func startTrojanInbound(t *testing.T, fallback *inbound.Fallback) (net.Addr, recordingHandler) {
	return startCountingTrojanInbound(t, fallback, nil)
}

func startCountingTrojanInbound(t *testing.T, fallback *inbound.Fallback, counters *stats.InboundCounters) (net.Addr, recordingHandler) {
	t.Helper()
	transport := tcp.NewBaseStreamTransportModule()
	listener, err := transport.NewStreamListener(loopback(t))
	if err != nil {
		t.Fatal(err)
	}
	proxy := &trojan.Proxy{Settings: trojan.Settings{
		Users:            []trojan.User{{Name: "alice", Password: "secret"}},
		HandshakeTimeout: types.Pointer(types.Duration(500 * time.Millisecond)),
	}}
	streamServer, err := proxy.NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	datagramServer, err := proxy.NewDatagramOverStreamServer()
	if err != nil {
		t.Fatal(err)
	}

	in := inbound.NewStream(inbound.StreamConfig{
		Tag: "trojan-in", Listener: listener, ProxyServer: streamServer,
		DatagramOverStreamServer: datagramServer, Fallback: fallback, Counters: counters,
	})
	handler := recordingHandler{calls: make(chan handled, 1)}
	if err := in.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return listener.LocalAddr(), handler
}

func request(t *testing.T, command byte) []byte {
	destination, _ := spec.NewDestinationFromDomain("example.com", 443)
	header, err := wire.AppendRequest(nil, wire.Request{PasswordHash: wire.HashPassword("secret"), Command: command, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	return header
}

func TestEachRequestReachesTheMatchingHandler(t *testing.T) {
	address, handler := startTrojanInbound(t, nil)

	for _, tc := range []struct {
		command byte
		want    spec.Network
	}{
		{wire.CommandConnect, spec.NetworkTCP},
		{wire.CommandUDPAssociate, spec.NetworkUDP},
	} {
		connection, err := net.Dial("tcp", address.String())
		if err != nil {
			t.Fatal(err)
		}
		connection.Write(request(t, tc.command))

		select {
		case got := <-handler.calls:
			if got.network != tc.want || got.metadata.User != "alice" || got.metadata.InboundTag != "trojan-in" {
				t.Errorf("command X'%02X' dispatched as %+v", tc.command, got)
			}
			if got.metadata.Transport.InboundLocalEndpoint.String() != address.String() {
				t.Errorf("command X'%02X': InboundLocalEndpoint = %s, want the address the client reached, %s",
					tc.command, got.metadata.Transport.InboundLocalEndpoint, address)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("command X'%02X' never reached the handler", tc.command)
		}
		connection.Close()
	}
}

// The fallback must see exactly what the client sent, from its first byte,
// though the protocol had already read part of it before refusing.
func TestARefusedStreamIsReplayedWholeToTheFallback(t *testing.T) {
	fallbackListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fallbackListener.Close()
	received := make(chan []byte, 1)
	go func() {
		connection, err := fallbackListener.Accept()
		if err != nil {
			return
		}
		data, _ := io.ReadAll(connection)
		received <- data
		connection.Close()
	}()

	fallbackAddress := fallbackListener.Addr().(*net.TCPAddr).AddrPort()
	fallbackDestination, _ := spec.NewDestinationFromIP(fallbackAddress.Addr(), fallbackAddress.Port())
	dialer, err := tcp.NewBaseStreamTransportModule().NewStreamDialer(dns.NewSystemResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	address, handler := startTrojanInbound(t, &inbound.Fallback{Dialer: dialer, Destination: fallbackDestination})

	probe := []byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: a-probe-long-enough-to-pass-the-command-offset\r\n\r\n")
	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	connection.Write(probe)
	connection.(*net.TCPConn).CloseWrite()

	select {
	case got := <-received:
		if !bytes.Equal(got, probe) {
			t.Errorf("fallback received %q, want %q", got, probe)
		}
	case call := <-handler.calls:
		t.Fatalf("a probe reached the handler: %+v", call)
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the fallback")
	}
	connection.Close()
}

func TestWithoutAFallbackARefusedStreamIsClosed(t *testing.T) {
	address, _ := startTrojanInbound(t, nil)

	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write(bytes.Repeat([]byte("x"), 100))
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))

	if _, err := connection.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Errorf("read = %v, want the connection closed", err)
	}
}

func isTimeout(err error) bool {
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// acceptingServer hands every stream straight through, with no handshake.
type acceptingServer struct{}

func (acceptingServer) AcceptStream(_ context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	return underlyingStream, spec.Metadata{Network: spec.NetworkTCP}, nil
}

// holdingHandler keeps each stream open until the test lets it go.
type holdingHandler struct {
	started chan struct{}
	release chan struct{}
}

func (h holdingHandler) HandleStream(_ context.Context, c spec.Stream, _ spec.Metadata) error {
	h.started <- struct{}{}
	<-h.release
	return c.Close()
}

func (holdingHandler) HandleDatagram(_ context.Context, c spec.Datagram, _ spec.Metadata) error {
	return c.Close()
}

func startLimitedInbound(t *testing.T, maxConnections int) (net.Addr, holdingHandler) {
	return startCountingLimitedInbound(t, maxConnections, nil)
}

func startCountingLimitedInbound(t *testing.T, maxConnections int, counters *stats.InboundCounters) (net.Addr, holdingHandler) {
	t.Helper()
	listener, err := tcp.NewBaseStreamTransportModule().NewStreamListener(loopback(t))
	if err != nil {
		t.Fatal(err)
	}
	in := inbound.NewStream(inbound.StreamConfig{
		Tag: "limited", Listener: listener, ProxyServer: acceptingServer{}, MaxTCPConnections: maxConnections, Counters: counters,
	})
	handler := holdingHandler{started: make(chan struct{}, 4), release: make(chan struct{})}
	if err := in.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(handler.release)
		in.Close()
	})
	return listener.LocalAddr(), handler
}

func dialLimited(t *testing.T, address net.Addr) net.Conn {
	t.Helper()
	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	t.Cleanup(func() { connection.Close() })
	return connection
}

func awaitStarted(t *testing.T, handler holdingHandler) {
	t.Helper()
	select {
	case <-handler.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection never reached the handler")
	}
}

func TestAConnectionBeyondMaxTCPConnectionsIsClosedAtOnce(t *testing.T) {
	address, handler := startLimitedInbound(t, 1)

	dialLimited(t, address)
	awaitStarted(t, handler)

	refused := dialLimited(t, address)
	if _, err := refused.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("read on the connection beyond the limit = %v, want EOF", err)
	}
	select {
	case <-handler.started:
		t.Error("the connection beyond the limit reached the handler")
	default:
	}
}

func TestASlotFreedByAnEndedConnectionIsReused(t *testing.T) {
	address, handler := startLimitedInbound(t, 1)

	dialLimited(t, address)
	awaitStarted(t, handler)
	handler.release <- struct{}{}

	// The slot is released after the handler returns, which races this
	// dial; a refused attempt is retried until the slot is free.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connection := dialLimited(t, address)
		select {
		case <-handler.started:
			return
		case <-time.After(100 * time.Millisecond):
			connection.Close()
		}
	}
	t.Fatal("no connection was accepted after the first one ended")
}

func TestZeroMaxTCPConnectionsMeansNoLimit(t *testing.T) {
	address, handler := startLimitedInbound(t, 0)
	for range 3 {
		dialLimited(t, address)
		awaitStarted(t, handler)
	}
}

func awaitCount(t *testing.T, description string, count func() uint64, want uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if count() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s = %d, want %d", description, count(), want)
}

func TestAConnectionBeyondMaxTCPConnectionsIsCountedAsRefused(t *testing.T) {
	var counters stats.InboundCounters
	address, handler := startCountingLimitedInbound(t, 1, &counters)

	dialLimited(t, address)
	awaitStarted(t, handler)
	refused := dialLimited(t, address)
	refused.Read(make([]byte, 1))

	awaitCount(t, "refused connections", func() uint64 { return counters.Snapshot().RefusedConnections }, 1)
	if got := counters.Snapshot().RefusalsByReason; got["max_tcp_connections"] != 1 {
		t.Errorf("reasons = %v, want max_tcp_connections once", got)
	}
}

func refusedFor(counters *stats.InboundCounters, reason stats.RefusalReason) func() uint64 {
	return func() uint64 { return counters.Snapshot().RefusalsByReason[reason.String()] }
}

func trojanRequest(t *testing.T, password string) []byte {
	t.Helper()
	destination, err := spec.NewDestinationFromDomain("example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	header, err := wire.AppendRequest(nil, wire.Request{PasswordHash: wire.HashPassword(password), Command: wire.CommandConnect, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	return header
}

func sendAndClose(t *testing.T, address net.Addr, payload []byte) {
	t.Helper()
	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write(payload)
	connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	io.Copy(io.Discard, connection)
}

func TestAWrongPasswordCountsAsAnAuthenticationFailureAndAsRefusedWithoutAFallback(t *testing.T) {
	var counters stats.InboundCounters
	address, _ := startCountingTrojanInbound(t, nil, &counters)

	sendAndClose(t, address, trojanRequest(t, "wrong"))

	awaitCount(t, "authentication failures", func() uint64 { return counters.Snapshot().AuthenticationFailures }, 1)
	awaitCount(t, "refused as a proxy handshake failure", refusedFor(&counters, stats.RefusalProxyHandshakeFailed), 1)
}

func TestGarbageIsRefusedButNotAnAuthenticationFailure(t *testing.T) {
	var counters stats.InboundCounters
	address, _ := startCountingTrojanInbound(t, nil, &counters)

	sendAndClose(t, address, bytes.Repeat([]byte("x"), 100))

	awaitCount(t, "refused as a proxy handshake failure", refusedFor(&counters, stats.RefusalProxyHandshakeFailed), 1)
	if got := counters.Snapshot().AuthenticationFailures; got != 0 {
		t.Errorf("authentication failures = %d, want 0 for garbage", got)
	}
}

func TestAWrongPasswordHandedToTheFallbackIsAnAuthenticationFailureAndAFallbackNotARefusal(t *testing.T) {
	fallbackListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fallbackListener.Close()
	go func() {
		connection, err := fallbackListener.Accept()
		if err != nil {
			return
		}
		io.Copy(io.Discard, connection)
		connection.Close()
	}()
	fallbackAddress := fallbackListener.Addr().(*net.TCPAddr).AddrPort()
	fallbackDestination, _ := spec.NewDestinationFromIP(fallbackAddress.Addr(), fallbackAddress.Port())
	dialer, err := tcp.NewBaseStreamTransportModule().NewStreamDialer(dns.NewSystemResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	var counters stats.InboundCounters
	address, _ := startCountingTrojanInbound(t, &inbound.Fallback{Dialer: dialer, Destination: fallbackDestination}, &counters)

	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write(trojanRequest(t, "wrong"))

	awaitCount(t, "authentication failures", func() uint64 { return counters.Snapshot().AuthenticationFailures }, 1)
	awaitCount(t, "connections sent to the fallback", func() uint64 { return counters.Snapshot().FallbackConnections }, 1)
	if got := counters.Snapshot().RefusedConnections; got != 0 {
		t.Errorf("refused = %d, want 0: the fallback served it", got)
	}
}

func TestAFallbackThatCannotBeReachedIsCountedAsARefusal(t *testing.T) {
	unreachable, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unreachableAddress := unreachable.Addr().(*net.TCPAddr).AddrPort()
	unreachable.Close()
	fallbackDestination, _ := spec.NewDestinationFromIP(unreachableAddress.Addr(), unreachableAddress.Port())
	dialer, err := tcp.NewBaseStreamTransportModule().NewStreamDialer(dns.NewSystemResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	var counters stats.InboundCounters
	address, _ := startCountingTrojanInbound(t, &inbound.Fallback{Dialer: dialer, Destination: fallbackDestination}, &counters)

	sendAndClose(t, address, bytes.Repeat([]byte("x"), 100))

	awaitCount(t, "refused as fallback unreachable", refusedFor(&counters, stats.RefusalFallbackUnreachable), 1)
}

func TestASilentClientIsRefusedAsATimedOutHandshake(t *testing.T) {
	var counters stats.InboundCounters
	address, _ := startCountingTrojanInbound(t, nil, &counters)

	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	awaitCount(t, "refused as a timed out handshake", refusedFor(&counters, stats.RefusalHandshakeTimedOut), 1)
}

// failingOverlay stands in for a transport overlay whose handshake fails.
type failingOverlay struct{ err error }

func (o failingOverlay) WrapServerStream(context.Context, spec.Stream, *spec.TransportMetadata) (spec.Stream, error) {
	return nil, o.err
}

func TestATransportHandshakeFailureIsCountedByItsStage(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want stats.RefusalReason
	}{
		{"an overlay that fails", errors.New("bad ClientHello"), stats.RefusalTransportHandshakeFailed},
		{"an overlay that times out", os.ErrDeadlineExceeded, stats.RefusalHandshakeTimedOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := tcp.NewBaseStreamTransportModule().NewStreamListener(loopback(t))
			if err != nil {
				t.Fatal(err)
			}
			var counters stats.InboundCounters
			in := inbound.NewStream(inbound.StreamConfig{
				Tag: "overlaid", Listener: listener, ProxyServer: acceptingServer{}, Counters: &counters,
				Overlays: []inbound.StreamOverlay{{Name: "stub", Server: failingOverlay{err: tc.err}}},
			})
			handler := holdingHandler{started: make(chan struct{}, 1), release: make(chan struct{})}
			if err := in.Activate(context.Background(), handler); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { close(handler.release); in.Close() })

			dialLimited(t, listener.LocalAddr())

			awaitCount(t, "refusals for "+tc.want.String(), refusedFor(&counters, tc.want), 1)
		})
	}
}

func TestAFailedHandshakeWithoutAFallbackIsCountedAsRefused(t *testing.T) {
	var counters stats.InboundCounters
	address, _ := startCountingTrojanInbound(t, nil, &counters)

	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write(bytes.Repeat([]byte("x"), 100))

	awaitCount(t, "refused connections", func() uint64 { return counters.Snapshot().RefusedConnections }, 1)
	if counters.Snapshot().FallbackConnections != 0 {
		t.Error("a connection with no fallback was counted as sent to one")
	}
}

func TestAStreamHandedToTheFallbackIsCountedAsSentToItNotRefused(t *testing.T) {
	fallbackListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fallbackListener.Close()
	go func() {
		connection, err := fallbackListener.Accept()
		if err != nil {
			return
		}
		io.Copy(io.Discard, connection)
		connection.Close()
	}()
	fallbackAddress := fallbackListener.Addr().(*net.TCPAddr).AddrPort()
	fallbackDestination, _ := spec.NewDestinationFromIP(fallbackAddress.Addr(), fallbackAddress.Port())
	dialer, err := tcp.NewBaseStreamTransportModule().NewStreamDialer(dns.NewSystemResolver(nil))
	if err != nil {
		t.Fatal(err)
	}
	var counters stats.InboundCounters
	address, _ := startCountingTrojanInbound(t, &inbound.Fallback{Dialer: dialer, Destination: fallbackDestination}, &counters)

	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: a-probe-long-enough-to-pass-the-command-offset\r\n\r\n"))

	awaitCount(t, "connections sent to the fallback", func() uint64 { return counters.Snapshot().FallbackConnections }, 1)
	if counters.Snapshot().RefusedConnections != 0 {
		t.Error("a connection the fallback served was also counted as refused")
	}
}
