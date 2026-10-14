package inbound_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/inbound"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/module/proxy/trojan"
	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/module/transport/base/tcp"
	"github.com/we-apranik/apranik/spec"
)

// A datagram that holds until it is closed, as a UDP session's socket does.
type silentDatagram struct {
	closed    chan struct{}
	closeOnce sync.Once
}

func newSilentDatagram() *silentDatagram { return &silentDatagram{closed: make(chan struct{})} }

func (d *silentDatagram) ReadFrom([]byte) (int, spec.Destination, error) {
	<-d.closed
	return 0, spec.Destination{}, io.EOF
}
func (d *silentDatagram) WriteTo(p []byte, _ spec.Destination) (int, error) { return len(p), nil }
func (d *silentDatagram) LocalAddr() net.Addr                               { return &net.UDPAddr{} }
func (d *silentDatagram) Close() error {
	d.closeOnce.Do(func() { close(d.closed) })
	return nil
}

// Holds each UDP session until its datagram is closed.
type sessionHoldingHandler struct{ started chan struct{} }

func (sessionHoldingHandler) HandleStream(_ context.Context, c spec.Stream, _ spec.Metadata) error {
	return c.Close()
}

func (h sessionHoldingHandler) HandleDatagram(_ context.Context, c spec.Datagram, _ spec.Metadata) error {
	h.started <- struct{}{}
	for {
		if _, _, err := c.ReadFrom(make([]byte, 1)); err != nil {
			return err
		}
	}
}

// A proxy server that asks for a UDP association on every connection, like SOCKS5.
type associatingServer struct{ openSocket *spec.UDPAssociateSocketFunc }

func (associatingServer) AcceptStream(context.Context, spec.Stream) (spec.Stream, spec.Metadata, error) {
	return nil, spec.Metadata{}, spec.ErrUDPAssociationRequested
}

func (s associatingServer) SetAssociateSocket(socket spec.UDPAssociateSocketFunc) {
	*s.openSocket = socket
}

func (s associatingServer) Associate(ctx context.Context, _ spec.Stream, _ spec.Metadata) (spec.Datagram, error) {
	return (*s.openSocket)(ctx)
}

func startAssociatingInbound(t *testing.T, maxUDPSessions int, openSocket spec.UDPAssociateSocketFunc, counters *stats.InboundCounters) (net.Addr, sessionHoldingHandler) {
	t.Helper()
	listener, err := tcp.NewBaseStreamTransportModule().NewStreamListener(loopback(t))
	if err != nil {
		t.Fatal(err)
	}
	in := inbound.NewStream(inbound.StreamConfig{
		Tag: "associating", Listener: listener, ProxyServer: associatingServer{openSocket: new(spec.UDPAssociateSocketFunc)},
		AssociateSocket: openSocket, MaxUDPSessions: maxUDPSessions, Counters: counters,
	})
	handler := sessionHoldingHandler{started: make(chan struct{}, 8)}
	if err := in.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return listener.LocalAddr(), handler
}

func openSilentDatagram(context.Context) (spec.Datagram, error) { return newSilentDatagram(), nil }

func awaitSessionStarted(t *testing.T, handler sessionHoldingHandler) {
	t.Helper()
	select {
	case <-handler.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the UDP session never reached the handler")
	}
}

func awaitClosedByInbound(t *testing.T, connection net.Conn) {
	t.Helper()
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := connection.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("read on a refused connection = %v, want EOF", err)
	}
}

func TestAnAssociationBeyondMaxUDPSessionsIsRefusedAndCounted(t *testing.T) {
	var counters stats.InboundCounters
	address, handler := startAssociatingInbound(t, 1, openSilentDatagram, &counters)

	dialLimited(t, address)
	awaitSessionStarted(t, handler)

	awaitClosedByInbound(t, dialLimited(t, address))
	select {
	case <-handler.started:
		t.Error("the association beyond the limit reached the handler")
	default:
	}
	awaitCount(t, "refusals for max_udp_sessions", refusedFor(&counters, stats.RefusalMaxUDPSessions), 1)
}

func TestTheSlotOfAnEndedAssociationIsReused(t *testing.T) {
	address, handler := startAssociatingInbound(t, 1, openSilentDatagram, nil)

	first := dialLimited(t, address)
	awaitSessionStarted(t, handler)
	first.Close()

	// The slot is given back after the control stream is seen closed, which
	// races this dial; a refused attempt is retried until it is free.
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
	t.Fatal("no association was accepted after the first one ended")
}

func TestAnAssociationWhoseSocketCannotBeOpenedGivesItsSlotBack(t *testing.T) {
	var attempts atomic.Int32
	openSocketFailingOnce := func(ctx context.Context) (spec.Datagram, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("no free port")
		}
		return newSilentDatagram(), nil
	}
	address, handler := startAssociatingInbound(t, 1, openSocketFailingOnce, nil)

	awaitClosedByInbound(t, dialLimited(t, address))
	dialLimited(t, address)
	awaitSessionStarted(t, handler)
}

func TestZeroMaxUDPSessionsMeansNoLimit(t *testing.T) {
	address, handler := startAssociatingInbound(t, 0, openSilentDatagram, nil)
	for range 5 {
		dialLimited(t, address)
		awaitSessionStarted(t, handler)
	}
}

func TestATrojanUDPSessionBeyondMaxUDPSessionsIsRefusedAndCounted(t *testing.T) {
	var counters stats.InboundCounters
	listener, err := tcp.NewBaseStreamTransportModule().NewStreamListener(loopback(t))
	if err != nil {
		t.Fatal(err)
	}
	proxy := &trojan.Proxy{Settings: trojan.Settings{Users: []trojan.User{{Name: "alice", Password: "secret"}}}}
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
		DatagramOverStreamServer: datagramServer, MaxUDPSessions: 1, Counters: &counters,
	})
	handler := sessionHoldingHandler{started: make(chan struct{}, 8)}
	if err := in.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })

	first := dialLimited(t, listener.LocalAddr())
	first.Write(request(t, wire.CommandUDPAssociate))
	awaitSessionStarted(t, handler)

	second := dialLimited(t, listener.LocalAddr())
	second.Write(request(t, wire.CommandUDPAssociate))
	awaitClosedByInbound(t, second)
	awaitCount(t, "refusals for max_udp_sessions", refusedFor(&counters, stats.RefusalMaxUDPSessions), 1)

	// A TCP connection is not a UDP session: it needs no UDP slot.
	connect := dialLimited(t, listener.LocalAddr())
	connect.Write(request(t, wire.CommandConnect))
	awaitCount(t, "refusals for max_udp_sessions", refusedFor(&counters, stats.RefusalMaxUDPSessions), 1)
}
