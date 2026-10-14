package handler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/route/handler"
	"github.com/we-apranik/apranik/core/route/router"
	"github.com/we-apranik/apranik/spec"
)

// An echo outbound that counts the datagrams it opens: the sockets the remote
// peer sees, so one open across a resume is the same source port.
type openCountingUDPOutbound struct {
	echoUDPOutbound
	opened atomic.Int32
}

func (o *openCountingUDPOutbound) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	o.opened.Add(1)
	return o.echoUDPOutbound.CreateDatagram(ctx, metadata)
}

func resumableMetadata(user string, globalID byte) spec.Metadata {
	metadata := udpMetadata(user)
	metadata.XUDPGlobalID = [8]byte{globalID}
	return metadata
}

func newResumingHandler(outbound *openCountingUDPOutbound, holdTime time.Duration) *handler.Handler {
	return handler.New(router.NewWithDefaultOutbound(outbound), handler.Config{ResumeUDPSessions: true, UDPSessionResumeHoldTime: holdTime})
}

func expectEnded(t *testing.T, session *udpSessionUnderTest, what string) {
	t.Helper()
	select {
	case err := <-session.ended:
		session.ended <- err // the cleanup of startUDPSession waits for it too
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: the HandleDatagram call did not return", what)
	}
}

func expectNotEnded(t *testing.T, session *udpSessionUnderTest, what string) {
	t.Helper()
	select {
	case err := <-session.ended:
		t.Fatalf("%s: the HandleDatagram call returned %v", what, err)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestAStreamThatReconnectsWithTheSameIDGetsItsSessionBackWithTheSameOutboundSocket(t *testing.T) {
	outbound := &openCountingUDPOutbound{echoUDPOutbound: echoUDPOutbound{tag: "out"}}
	trafficHandler := newResumingHandler(outbound, time.Minute)
	target := udpTarget(t, "192.0.2.1", 53)

	first := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "first")
	first.exchange(t, target)

	first.client.Close() // the stream is lost
	expectNotEnded(t, first, "the lost stream's call holds the session")

	second := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "second")
	second.exchange(t, target)
	if got := outbound.opened.Load(); got != 1 {
		t.Errorf("the outbound opened %d datagrams, want 1: the session was not resumed", got)
	}

	second.client.Close() // lost again, and resumed again
	third := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "third")
	third.exchange(t, target)
	expectEnded(t, second, "the call of a stream that is no longer the session's")
	if got := outbound.opened.Load(); got != 1 {
		t.Errorf("the outbound opened %d datagrams after a second resume, want 1", got)
	}
}

func TestAnotherUserWithTheSameIDDoesNotGetTheSession(t *testing.T) {
	outbound := &openCountingUDPOutbound{echoUDPOutbound: echoUDPOutbound{tag: "out"}}
	trafficHandler := newResumingHandler(outbound, time.Minute)
	target := udpTarget(t, "192.0.2.1", 53)

	alice := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "alice")
	alice.exchange(t, target)
	alice.client.Close()

	bob := startUDPSession(t, trafficHandler, resumableMetadata("bob", 1), "bob")
	bob.exchange(t, target)
	if got := outbound.opened.Load(); got != 2 {
		t.Errorf("the outbound opened %d datagrams, want 2: bob must not take alice's session", got)
	}
}

func TestASessionIsNotHeldPastTheHoldTime(t *testing.T) {
	outbound := &openCountingUDPOutbound{echoUDPOutbound: echoUDPOutbound{tag: "out"}}
	trafficHandler := newResumingHandler(outbound, 100*time.Millisecond)
	target := udpTarget(t, "192.0.2.1", 53)

	first := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "first")
	first.exchange(t, target)
	first.client.Close()

	expectEnded(t, first, "the hold ran out")
	second := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "second")
	second.exchange(t, target)
	if got := outbound.opened.Load(); got != 2 {
		t.Errorf("the outbound opened %d datagrams, want 2: the session had ended", got)
	}
}

func TestWithoutResumeALostStreamEndsItsSession(t *testing.T) {
	outbound := &openCountingUDPOutbound{echoUDPOutbound: echoUDPOutbound{tag: "out"}}
	trafficHandler := handler.New(router.NewWithDefaultOutbound(outbound), handler.Config{})
	target := udpTarget(t, "192.0.2.1", 53)

	first := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "first")
	first.exchange(t, target)
	first.client.Close()
	expectEnded(t, first, "a session that is not resumed")

	second := startUDPSession(t, trafficHandler, resumableMetadata("alice", 1), "second")
	second.exchange(t, target)
	if got := outbound.opened.Load(); got != 2 {
		t.Errorf("the outbound opened %d datagrams, want 2", got)
	}
}

func TestASessionWithoutAnIDIsNotHeldEvenWhenResumeIsOn(t *testing.T) {
	outbound := &openCountingUDPOutbound{echoUDPOutbound: echoUDPOutbound{tag: "out"}}
	trafficHandler := newResumingHandler(outbound, time.Minute)
	target := udpTarget(t, "192.0.2.1", 53)

	first := startUDPSession(t, trafficHandler, udpMetadata("alice"), "first")
	first.exchange(t, target)
	first.client.Close()
	expectEnded(t, first, "a session with no Global ID")
}
