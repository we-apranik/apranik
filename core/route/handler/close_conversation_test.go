package handler_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/route/handler"
	"github.com/we-apranik/apranik/core/route/router"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

func TestClosingALiveTCPConnectionEndsItQuietly(t *testing.T) {
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	registry := trackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})
	destination, _ := spec.NewDestinationFromDomain("example.com", 443)
	connection := openThrough(t, trafficHandler, out.dialed, spec.Metadata{
		Network: spec.NetworkTCP, InboundTag: "in", Destination: destination, Transport: clientAt(t, "203.0.113.7"),
	})
	defer connection.client.Close()
	defer connection.remote.Close()

	var listed []stats.ConversationSnapshot
	eventually(t, "the connection listed", func() bool {
		listed = registry.LiveConversations()
		return len(listed) == 1
	})

	if err := registry.CloseLiveConversation(listed[0].ID); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-connection.handlerReturned:
		if err != nil {
			t.Errorf("a connection closed by request returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return after the close")
	}
	connection.client.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := connection.client.Read(make([]byte, 1)); err != io.EOF && err != io.ErrClosedPipe {
		t.Errorf("the client read %v after the close, want the end of the stream", err)
	}
	if live := registry.LiveConversations(); len(live) != 0 {
		t.Errorf("%d connections still listed after the close", len(live))
	}
	if active := registry.InboundTraffic()["in"].TCP.ActiveConnections; active != 0 {
		t.Errorf("%d active connections counted after the close", active)
	}
}

func TestClosingOneUDPEntryEndsTheWholeUDPSession(t *testing.T) {
	registry := udpTrackingRegistry(stats.OutboundDescription{Tag: "out"})
	trafficHandler := handler.New(router.NewWithDefaultOutbound(echoUDPOutbound{tag: "out"}), handler.Config{Stats: registry})
	client := newClientDatagram("twelve bytes")
	ended := make(chan error, 1)
	go func() { ended <- trafficHandler.HandleDatagram(context.Background(), client, udpMetadata("alice")) }()
	defer client.Close()

	for _, target := range []spec.Destination{udpTarget(t, "192.0.2.1", 53), udpTarget(t, "192.0.2.2", 443)} {
		client.outgoing <- target
		select {
		case <-client.returned:
		case <-time.After(5 * time.Second):
			t.Fatal("no reply reached the client")
		}
	}
	var listed []stats.ConversationSnapshot
	eventually(t, "both destinations listed", func() bool {
		listed = registry.LiveConversations()
		return len(listed) == 2
	})

	if err := registry.CloseLiveConversation(listed[0].ID); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-ended:
		if err != nil {
			t.Errorf("a UDP session closed by request returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the UDP session did not end after one of its entries was closed")
	}
	if live := registry.LiveConversations(); len(live) != 0 {
		t.Errorf("%d entries still listed after the session ended", len(live))
	}
}
