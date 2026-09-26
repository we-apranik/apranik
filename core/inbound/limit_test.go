package inbound_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/inbound"
	"github.com/The-Apranik/Apranik/module/transport/tcp"
	"github.com/The-Apranik/Apranik/spec"
)

// acceptingServer hands every stream straight through, with no handshake.
type acceptingServer struct{}

func (acceptingServer) AcceptStream(_ context.Context, carrier spec.Stream) (spec.Stream, spec.Metadata, error) {
	return carrier, spec.Metadata{Network: spec.NetworkTCP}, nil
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
	t.Helper()
	listener, err := tcp.NewBaseStreamTransportModule().NewStreamListener(loopback(t))
	if err != nil {
		t.Fatal(err)
	}
	in := inbound.NewStream(inbound.StreamConfig{
		Tag: "limited", Listener: listener, ProxyServer: acceptingServer{}, MaxConnections: maxConnections,
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

func TestAConnectionBeyondMaxConnectionsIsClosedAtOnce(t *testing.T) {
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

func TestZeroMaxConnectionsMeansNoLimit(t *testing.T) {
	address, handler := startLimitedInbound(t, 0)
	for range 3 {
		dialLimited(t, address)
		awaitStarted(t, handler)
	}
}
