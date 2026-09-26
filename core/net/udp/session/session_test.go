// Package session_test drives a session against fake outbounds, where what it
// opened and closed is observable, and against the real freedom outbound over
// loopback, where the socket behaviour is real.
package session_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/net/udp/session"
	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/module/transport/udp"
	"github.com/The-Apranik/Apranik/spec"
)

const shortWait = 500 * time.Millisecond

func destinationOf(t *testing.T, host string, port uint16) spec.Destination {
	t.Helper()

	made, err := spec.NewDestinationFromIP(netip.MustParseAddr(host), port)
	if err != nil {
		t.Fatalf("NewDestinationFromIP: %v", err)
	}
	return made
}

type fakeDatagram struct {
	mu       sync.Mutex
	written  []spec.Destination
	closed   bool
	readFail error

	incoming chan spec.Destination
	done     chan struct{}
}

func newFakeDatagram() *fakeDatagram {
	return &fakeDatagram{incoming: make(chan spec.Destination, 8), done: make(chan struct{})}
}

func (f *fakeDatagram) WriteTo(payload []byte, to spec.Destination) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, errors.New("fake: closed")
	}
	f.written = append(f.written, to)
	return len(payload), nil
}

func (f *fakeDatagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	select {
	case from := <-f.incoming:
		f.mu.Lock()
		failure := f.readFail
		f.mu.Unlock()
		if failure != nil {
			return 0, spec.Destination{}, failure
		}
		return copy(payload, []byte("reply")), from, nil
	case <-f.done:
		return 0, spec.Destination{}, errors.New("fake: closed")
	}
}

func (f *fakeDatagram) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	return nil
}

func (f *fakeDatagram) LocalAddr() net.Addr { return nil }

func (f *fakeDatagram) wasClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeDatagram) failNextRead(err error) {
	f.mu.Lock()
	f.readFail = err
	f.mu.Unlock()
	f.incoming <- spec.Destination{}
}

func (f *fakeDatagram) writes() []spec.Destination {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]spec.Destination(nil), f.written...)
}

type fakeOutbound struct {
	tag      string
	networks []spec.Network
	dialErr  error
	slowDial time.Duration

	dials    atomic.Int32
	mu       sync.Mutex
	carriers []*fakeDatagram
}

func newFakeOutbound(tag string) *fakeOutbound {
	return &fakeOutbound{tag: tag, networks: []spec.Network{spec.NetworkTCP, spec.NetworkUDP}}
}

func (o *fakeOutbound) Tag() string              { return o.tag }
func (o *fakeOutbound) Networks() []spec.Network { return o.networks }
func (o *fakeOutbound) Close() error             { return nil }

func (o *fakeOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return nil, spec.ErrUnsupportedNetwork
}

func (o *fakeOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	o.dials.Add(1)
	if o.slowDial > 0 {
		time.Sleep(o.slowDial)
	}
	if o.dialErr != nil {
		return nil, o.dialErr
	}

	carrier := newFakeDatagram()
	o.mu.Lock()
	o.carriers = append(o.carriers, carrier)
	o.mu.Unlock()
	return carrier, nil
}

func (o *fakeOutbound) carrierCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.carriers)
}

func (o *fakeOutbound) survivingCarrierCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	surviving := 0
	for _, carrier := range o.carriers {
		if !carrier.wasClosed() {
			surviving++
		}
	}
	return surviving
}

func (o *fakeOutbound) carrierAt(t *testing.T, index int) *fakeDatagram {
	t.Helper()

	o.mu.Lock()
	defer o.mu.Unlock()
	if index >= len(o.carriers) {
		t.Fatalf("outbound %q opened %d carriers, wanted at least %d", o.tag, len(o.carriers), index+1)
	}
	return o.carriers[index]
}

func newSession(t *testing.T, config session.Config) *session.Session {
	t.Helper()

	udpSession := session.New(context.Background(), config)
	t.Cleanup(func() { udpSession.Close() })
	return udpSession
}

func expectReply(t *testing.T, udpSession *session.Session, wait time.Duration) session.Reply {
	t.Helper()

	select {
	case reply := <-udpSession.Replies():
		return reply
	case <-time.After(wait):
		t.Fatal("no reply arrived")
		return session.Reply{}
	}
}

func TestSendThroughTwoOutboundsMergesBothReplies(t *testing.T) {
	dns := newFakeOutbound("dns-freedom")
	game := newFakeOutbound("game-proxy")
	udpSession := newSession(t, session.Config{})

	dnsAddr := destinationOf(t, "192.0.2.1", 53)
	gameAddr := destinationOf(t, "198.51.100.7", 27015)

	if err := udpSession.Send([]byte("q"), dnsAddr, dns); err != nil {
		t.Fatalf("Send via dns: %v", err)
	}
	if err := udpSession.Send([]byte("g"), gameAddr, game); err != nil {
		t.Fatalf("Send via game: %v", err)
	}

	if writes := dns.carrierAt(t, 0).writes(); len(writes) != 1 || writes[0] != dnsAddr {
		t.Errorf("dns outbound saw %v, want [%v]", writes, dnsAddr)
	}
	if writes := game.carrierAt(t, 0).writes(); len(writes) != 1 || writes[0] != gameAddr {
		t.Errorf("game outbound saw %v, want [%v]", writes, gameAddr)
	}

	dns.carrierAt(t, 0).incoming <- dnsAddr
	game.carrierAt(t, 0).incoming <- gameAddr

	seen := map[spec.Destination]bool{}
	for range 2 {
		seen[expectReply(t, udpSession, shortWait).From] = true
	}
	if !seen[dnsAddr] || !seen[gameAddr] {
		t.Errorf("merged replies were %v, want both %v and %v", seen, dnsAddr, gameAddr)
	}
}

func TestSendThroughANonDatagramOutboundDialsNothing(t *testing.T) {
	streamOnly := newFakeOutbound("proxy")
	streamOnly.networks = []spec.Network{spec.NetworkTCP}
	udpSession := newSession(t, session.Config{})

	err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), streamOnly)
	if !errors.Is(err, spec.ErrUnsupportedNetwork) {
		t.Errorf("Send through a stream-only outbound: %v, want spec.ErrUnsupportedNetwork", err)
	}
	if dials := int(streamOnly.dials.Load()); dials != 0 {
		t.Errorf("the outbound was dialed %d times, want 0: capability is checked before dispatch", dials)
	}
}

func TestRacingFirstSendsOpenOneOutboundDatagram(t *testing.T) {
	out := newFakeOutbound("freedom")
	out.slowDial = 50 * time.Millisecond
	udpSession := newSession(t, session.Config{})

	target := destinationOf(t, "192.0.2.1", 53)

	var senders sync.WaitGroup
	for range 16 {
		senders.Add(1)
		go func() {
			defer senders.Done()
			udpSession.Send([]byte("x"), target, out)
		}()
	}
	senders.Wait()

	// Dialing happens outside the lock, so several may be dialed; only one
	// may survive, the losers being closed.
	if surviving := out.survivingCarrierCount(); surviving != 1 {
		t.Errorf("16 racing sends left %d carriers alive out of %d dialed, want 1", surviving, out.carrierCount())
	}
}

func TestAQuietOutboundDatagramStaysOpenUntilTheSessionEnds(t *testing.T) {
	out := newFakeOutbound("freedom")
	udpSession := newSession(t, session.Config{IdleTimeout: 5 * time.Second})

	if err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), out); err != nil {
		t.Fatalf("Send: %v", err)
	}
	carrier := out.carrierAt(t, 0)

	time.Sleep(200 * time.Millisecond)
	if carrier.wasClosed() {
		t.Fatal("a quiet outbound datagram was closed while its session was alive")
	}

	udpSession.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !carrier.wasClosed() {
		time.Sleep(10 * time.Millisecond)
	}
	if !carrier.wasClosed() {
		t.Error("closing the session left its outbound datagram open")
	}
}

func TestAnOutboundDatagramWhoseReadsFailIsReopened(t *testing.T) {
	out := newFakeOutbound("freedom")
	udpSession := newSession(t, session.Config{})

	target := destinationOf(t, "192.0.2.1", 53)
	if err := udpSession.Send([]byte("x"), target, out); err != nil {
		t.Fatalf("Send: %v", err)
	}
	first := out.carrierAt(t, 0)

	first.failNextRead(errors.New("carrier dropped"))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !first.wasClosed() {
		time.Sleep(10 * time.Millisecond)
	}
	if !first.wasClosed() {
		t.Fatal("a carrier whose reads failed was not removed")
	}

	if err := udpSession.Send([]byte("y"), target, out); err != nil {
		t.Fatalf("Send after the carrier died: %v", err)
	}
	if carriers := out.carrierCount(); carriers != 2 {
		t.Errorf("the outbound opened %d carriers, want 2: a dead carrier must be reopened", carriers)
	}
}

func TestAQuietSessionEndsWithTheIdleReason(t *testing.T) {
	out := newFakeOutbound("freedom")
	udpSession := newSession(t, session.Config{IdleTimeout: 80 * time.Millisecond})

	if err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), out); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case <-udpSession.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a session left quiet did not end")
	}
	if !errors.Is(udpSession.Err(), session.ErrIdle) {
		t.Errorf("Err() = %v, want session.ErrIdle", udpSession.Err())
	}
	// Done closes first and the carriers right after it, so give the close a
	// moment instead of racing it.
	carrier := out.carrierAt(t, 0)
	deadline := time.Now().Add(2 * time.Second)
	for !carrier.wasClosed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !carrier.wasClosed() {
		t.Error("an idled-out session left its carrier open")
	}
}

func TestCloseEndsWithTheClosedReasonAndLeaksNothing(t *testing.T) {
	first := newFakeOutbound("one")
	second := newFakeOutbound("two")

	udpSession := session.New(context.Background(), session.Config{})

	udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), first)
	udpSession.Send([]byte("x"), destinationOf(t, "198.51.100.7", 27015), second)

	if udpSession.Err() != nil {
		t.Errorf("Err() = %v on a live session, want nil", udpSession.Err())
	}

	before := runtime.NumGoroutine()
	udpSession.Close()

	if !errors.Is(udpSession.Err(), session.ErrClosed) {
		t.Errorf("Err() = %v, want session.ErrClosed", udpSession.Err())
	}
	if !first.carrierAt(t, 0).wasClosed() || !second.carrierAt(t, 0).wasClosed() {
		t.Error("Close left an outbound carrier open")
	}
	if err := udpSession.Send([]byte("x"), destinationOf(t, "192.0.2.1", 53), first); !errors.Is(err, session.ErrClosed) {
		t.Errorf("Send after Close: %v, want session.ErrClosed", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && runtime.NumGoroutine() > before {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("%d goroutines before Close, %d after: reply loops leaked", before, after)
	}
}

// One NAT per session, over the real freedom outbound.
func TestTwoSessionsToOneDestinationUseDifferentSockets(t *testing.T) {
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer peer.Close()

	local := peer.LocalAddr().(*net.UDPAddr)
	target := destinationOf(t, "127.0.0.1", uint16(local.Port))

	dialer, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	freedom := outbound.NewFreedom(outbound.FreedomConfig{Tag: "freedom", DatagramDialer: dialer})

	firstSession := newSession(t, session.Config{})
	secondSession := newSession(t, session.Config{})

	if err := firstSession.Send([]byte("first"), target, freedom); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	firstSource := readSource(t, peer)

	if err := secondSession.Send([]byte("second"), target, freedom); err != nil {
		t.Fatalf("second Send: %v", err)
	}
	secondSource := readSource(t, peer)

	if firstSource.Port == secondSource.Port {
		t.Fatalf("both sessions egressed from port %d: one NAT is shared across sessions", firstSource.Port)
	}

	if _, err := peer.WriteToUDP([]byte("reply"), firstSource); err != nil {
		t.Fatalf("replying: %v", err)
	}
	if reply := expectReply(t, firstSession, 2*time.Second); len(reply.Payload) == 0 {
		t.Error("the first session received an empty reply")
	}

	arrived := make(chan struct{})
	go func() {
		select {
		case <-secondSession.Replies():
			close(arrived)
		case <-time.After(shortWait):
		}
	}()
	select {
	case <-arrived:
		t.Error("the second session received the first session's reply")
	case <-time.After(shortWait + 100*time.Millisecond):
	}
}

func readSource(t *testing.T, peer *net.UDPConn) *net.UDPAddr {
	t.Helper()

	peer.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer peer.SetReadDeadline(time.Time{})

	_, from, err := peer.ReadFromUDP(make([]byte, 128))
	if err != nil {
		t.Fatalf("reading what the session sent: %v", err)
	}
	return from
}
