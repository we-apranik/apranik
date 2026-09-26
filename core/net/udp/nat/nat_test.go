// Package nat_test exercises nat against real loopback UDP sockets,
// matching how transport/udp and proxy/socks5's association tests already do
// it in this codebase, rather than against a fake spec.Datagram.
package nat_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/module/transport/udp"
	"github.com/The-Apranik/Apranik/spec"
)

// countingDialer wraps transport/udp's real dialer so tests can assert how
// many times a Datagram actually dialed.
func countingDialer(t *testing.T) (nat.CreateDatagramFunc, func() int) {
	t.Helper()

	real, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}

	var calls atomic.Int32
	dial := func(ctx context.Context) (spec.Datagram, error) {
		calls.Add(1)
		return real.CreateDatagram(ctx, spec.Endpoint{})
	}
	return dial, func() int { return int(calls.Load()) }
}

// destination stands up a real UDP socket to send from and receive at, so
// tests exercise genuine wire behavior rather than an in-memory stand-in.
func destination(t *testing.T, ip string) (*net.UDPConn, spec.Destination) {
	t.Helper()

	connection, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(ip)})
	if err != nil {
		t.Fatalf("ListenUDP on %s: %v", ip, err)
	}
	t.Cleanup(func() { connection.Close() })

	local := connection.LocalAddr().(*net.UDPAddr)
	return connection, spec.NewEndpointFromNetAddr(local).Destination()
}

// sourceSeenBy reports the address the nat datagram's packet actually came from,
// which is what a real peer replies to. natDatagram.LocalAddr() is a wildcard bind
// -- [::] or 0.0.0.0 -- and is not an address anyone can send back to.
func sourceSeenBy(t *testing.T, peer *net.UDPConn) *net.UDPAddr {
	t.Helper()

	peer.SetReadDeadline(time.Now().Add(shortWait))
	defer peer.SetReadDeadline(time.Time{})

	_, from, err := peer.ReadFromUDP(make([]byte, 64))
	if err != nil {
		t.Fatalf("reading the packet the nat datagram sent: %v", err)
	}
	return from
}

// A send that fails silently would look exactly like a packet the filtering
// policy dropped, so every send is checked.
func mustSend(t *testing.T, peer *net.UDPConn, payload string, to *net.UDPAddr) {
	t.Helper()

	if _, err := peer.WriteToUDP([]byte(payload), to); err != nil {
		t.Fatalf("sending %q to %s: %v", payload, to, err)
	}
}

func policy(mapping nat.MappingBehavior, filtering nat.FilteringBehavior) nat.Policy {
	return mustPolicy(mapping, filtering, time.Minute)
}

func mustPolicy(mapping nat.MappingBehavior, filtering nat.FilteringBehavior, idleTimeout time.Duration) nat.Policy {
	built, err := nat.NewPolicy(mapping, filtering, idleTimeout)
	if err != nil {
		panic(err)
	}
	return built
}

func newNAT(t *testing.T, dial nat.CreateDatagramFunc, p nat.Policy) *nat.Datagram {
	t.Helper()

	natDatagram, err := nat.New(context.Background(), dial, p, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return natDatagram
}

// startReading runs exactly one goroutine looping natDatagram.ReadFrom for the rest
// of the test, matching how relay.Datagram actually uses a spec.Datagram: one
// continuous reader, never a fresh one per call. Spawning a new reader per
// assertion would race: a call that times out still leaves its ReadFrom
// blocked, and a later packet can be delivered to that abandoned goroutine
// instead of the one a later assertion is waiting on.
func startReading(t *testing.T, natDatagram *nat.Datagram) <-chan spec.Destination {
	t.Helper()

	arrivals := make(chan spec.Destination, 8)
	go func() {
		buffer := make([]byte, 1500)
		for {
			_, from, err := natDatagram.ReadFrom(buffer)
			if err != nil {
				return
			}
			arrivals <- from
		}
	}()
	return arrivals
}

// expectArrival asserts a packet shows up within wait and returns its sender.
func expectArrival(t *testing.T, arrivals <-chan spec.Destination, wait time.Duration) *spec.Destination {
	t.Helper()

	select {
	case from := <-arrivals:
		return &from
	case <-time.After(wait):
		return nil
	}
}

// expectNothing asserts no packet shows up within wait.
func expectNothing(t *testing.T, arrivals <-chan spec.Destination, wait time.Duration) {
	t.Helper()

	select {
	case from := <-arrivals:
		t.Errorf("a packet arrived from %+v, want none", from)
	case <-time.After(wait):
	}
}

const shortWait = 300 * time.Millisecond

func TestMappingUnderEndpointIndependentSharesOneSocket(t *testing.T) {
	dial, dialCount := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.EndpointIndependentFiltering))
	defer natDatagram.Close()

	_, destinationA := destination(t, "127.0.0.1")
	_, destinationB := destination(t, "127.0.0.2")

	if _, err := natDatagram.WriteTo([]byte("a"), destinationA); err != nil {
		t.Fatalf("WriteTo A: %v", err)
	}
	if _, err := natDatagram.WriteTo([]byte("b"), destinationB); err != nil {
		t.Fatalf("WriteTo B: %v", err)
	}

	if got := dialCount(); got != 1 {
		t.Errorf("dial count = %d, want 1: EndpointIndependentMapping should share one socket across destinations", got)
	}
}

func TestMappingUnderAddressDependentSharesPortsOnOneHost(t *testing.T) {
	dial, dialCount := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.AddressDependentMapping, nat.EndpointIndependentFiltering))
	defer natDatagram.Close()

	_, portOne := destination(t, "127.0.0.1")
	_, portTwo := destination(t, "127.0.0.1")
	_, otherHost := destination(t, "127.0.0.2")

	natDatagram.WriteTo([]byte("x"), portOne)
	natDatagram.WriteTo([]byte("x"), portTwo)
	if got := dialCount(); got != 1 {
		t.Errorf("dial count = %d, want 1: AddressDependentMapping should share a socket across ports on one host", got)
	}

	natDatagram.WriteTo([]byte("x"), otherHost)
	if got := dialCount(); got != 2 {
		t.Errorf("dial count = %d, want 2: a different host must get its own socket", got)
	}
}

func TestMappingUnderAddressAndPortDependentOpensOnePerDestination(t *testing.T) {
	dial, dialCount := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.AddressAndPortDependentMapping, nat.EndpointIndependentFiltering))
	defer natDatagram.Close()

	_, portOne := destination(t, "127.0.0.1")
	_, portTwo := destination(t, "127.0.0.1")

	natDatagram.WriteTo([]byte("x"), portOne)
	if got := dialCount(); got != 1 {
		t.Fatalf("dial count = %d, want 1 after the first destination", got)
	}

	natDatagram.WriteTo([]byte("x"), portTwo)
	if got := dialCount(); got != 2 {
		t.Errorf("dial count = %d, want 2: AddressAndPortDependentMapping must open a new socket per port even on the same host", got)
	}

	// Writing to portOne again must reuse the entry already open for it, not
	// dial a third time.
	natDatagram.WriteTo([]byte("x"), portOne)
	if got := dialCount(); got != 2 {
		t.Errorf("dial count = %d, want 2: a destination already mapped must not be re-dialed", got)
	}
}

func TestFilteringUnderEndpointIndependentAcceptsAnySender(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.EndpointIndependentFiltering))
	defer natDatagram.Close()

	contacted, contactedAddr := destination(t, "127.0.0.1")
	stranger, _ := destination(t, "127.0.0.2")
	arrivals := startReading(t, natDatagram)

	if _, err := natDatagram.WriteTo([]byte("hello"), contactedAddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	tableAddr := sourceSeenBy(t, contacted)

	// The contacted destination's reply must always get through.
	mustSend(t, contacted, "reply", tableAddr)
	if from := expectArrival(t, arrivals, shortWait); from == nil {
		t.Fatal("reply from the contacted destination did not arrive")
	}

	// Under EndpointIndependentFiltering, an uncontacted stranger also gets
	// through -- this is the permissive, least-secure behavior being tested.
	mustSend(t, stranger, "uninvited", tableAddr)
	if from := expectArrival(t, arrivals, shortWait); from == nil {
		t.Error("a packet from an uncontacted sender did not arrive under EndpointIndependentFiltering, want it to")
	}
}

func TestFilteringUnderAddressAndPortDependentRejectsEverythingElse(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering))
	defer natDatagram.Close()

	contacted, contactedAddr := destination(t, "127.0.0.1")
	samehostDifferentPort, _ := destination(t, "127.0.0.1")
	stranger, _ := destination(t, "127.0.0.2")
	arrivals := startReading(t, natDatagram)

	if _, err := natDatagram.WriteTo([]byte("hello"), contactedAddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	tableAddr := sourceSeenBy(t, contacted)

	mustSend(t, samehostDifferentPort, "wrong port", tableAddr)
	expectNothing(t, arrivals, shortWait)

	mustSend(t, stranger, "wrong host", tableAddr)
	expectNothing(t, arrivals, shortWait)

	mustSend(t, contacted, "legitimate reply", tableAddr)
	if from := expectArrival(t, arrivals, shortWait); from == nil {
		t.Error("a reply from the exact contacted address:port did not arrive under AddressAndPortDependentFiltering")
	}
}

func TestFilteringUnderAddressDependentAllowsAnyPortOnAContactedHost(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.AddressDependentFiltering))
	defer natDatagram.Close()

	contacted, contactedAddr := destination(t, "127.0.0.1")
	samehostDifferentPort, _ := destination(t, "127.0.0.1")
	stranger, _ := destination(t, "127.0.0.2")
	arrivals := startReading(t, natDatagram)

	if _, err := natDatagram.WriteTo([]byte("hello"), contactedAddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	tableAddr := sourceSeenBy(t, contacted)

	mustSend(t, samehostDifferentPort, "same host", tableAddr)
	if from := expectArrival(t, arrivals, shortWait); from == nil {
		t.Error("a packet from an uncontacted port on the contacted host did not arrive under AddressDependentFiltering, want it accepted")
	}

	mustSend(t, stranger, "different host", tableAddr)
	expectNothing(t, arrivals, shortWait)
}

func TestIdleEntryIsReapedAndRedialedOnNextUse(t *testing.T) {
	dial, dialCount := countingDialer(t)
	shortPolicy := mustPolicy(nat.EndpointIndependentMapping, nat.EndpointIndependentFiltering, 50*time.Millisecond)
	natDatagram := newNAT(t, dial, shortPolicy)
	defer natDatagram.Close()

	_, destinationAddr := destination(t, "127.0.0.1")

	if _, err := natDatagram.WriteTo([]byte("x"), destinationAddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := dialCount(); got != 1 {
		t.Fatalf("dial count = %d, want 1", got)
	}

	time.Sleep(200 * time.Millisecond) // well past the 50ms idle timeout

	if _, err := natDatagram.WriteTo([]byte("y"), destinationAddr); err != nil {
		t.Fatalf("WriteTo after idle reap: %v", err)
	}
	if got := dialCount(); got != 2 {
		t.Errorf("dial count = %d, want 2: an idle-reaped entry must be re-dialed on next use", got)
	}
}

func TestCloseEndsTheSocketsItDialed(t *testing.T) {
	real, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}

	var lastDialed spec.Datagram
	dial := func(ctx context.Context) (spec.Datagram, error) {
		socket, err := real.CreateDatagram(ctx, spec.Endpoint{})
		lastDialed = socket
		return socket, err
	}

	natDatagram := newNAT(t, dial, nat.NewDefaultPolicy())
	_, destinationAddr := destination(t, "127.0.0.1")
	if _, err := natDatagram.WriteTo([]byte("x"), destinationAddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if err := natDatagram.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The dialed socket should already be closed: writing to it now must fail.
	if _, err := lastDialed.WriteTo([]byte("x"), destinationAddr); err == nil {
		t.Error("the socket Table dialed still accepted writes after Close")
	}
}

func TestReadFromReturnsErrClosedAfterClose(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, nat.NewDefaultPolicy())
	natDatagram.Close()

	buffer := make([]byte, 64)
	if _, _, err := natDatagram.ReadFrom(buffer); err != nat.ErrClosed {
		t.Errorf("ReadFrom after Close returned %v, want nat.ErrClosed", err)
	}
}

func TestDefaultPolicy(t *testing.T) {
	got := nat.NewDefaultPolicy()

	if got.Mapping() != nat.EndpointIndependentMapping {
		t.Errorf("default Mapping = %v, want EndpointIndependentMapping", got.Mapping())
	}
	if got.Filtering() != nat.AddressAndPortDependentFiltering {
		t.Errorf("default Filtering = %v, want AddressAndPortDependentFiltering", got.Filtering())
	}
	if got.IdleTimeout() != nat.DefaultIdleTimeout {
		t.Errorf("default IdleTimeout = %v, want %v", got.IdleTimeout(), nat.DefaultIdleTimeout)
	}
}

func TestNewRejectsAZeroPolicy(t *testing.T) {
	dial, _ := countingDialer(t)

	if _, err := nat.New(context.Background(), dial, nat.Policy{}, nil); err == nil {
		t.Error("a zero Policy was accepted, want an error")
	}
}

func TestNewPolicyRefusesNonsense(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mapping     nat.MappingBehavior
		filtering   nat.FilteringBehavior
		idleTimeout time.Duration
	}{
		{"unknown mapping", "per-destination", nat.AddressAndPortDependentFiltering, time.Minute},
		{"unknown filtering", nat.EndpointIndependentMapping, "strict", time.Minute},
		{"zero idle timeout", nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering, 0},
		{"negative idle timeout", nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering, -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := nat.NewPolicy(tc.mapping, tc.filtering, tc.idleTimeout); err == nil {
				t.Error("accepted, want an error")
			}
		})
	}
}

// Table and module/transport/udp both implement spec.Datagram, so they must
// agree that a short buffer is an error rather than a silent truncation.
func TestReadFromReportsATruncatedDatagram(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.EndpointIndependentFiltering))
	defer natDatagram.Close()

	peer, destinationAddr := destination(t, "127.0.0.1")

	if _, err := natDatagram.WriteTo([]byte("x"), destinationAddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	mustSend(t, peer, strings.Repeat("a", 1200), sourceSeenBy(t, peer))

	if _, _, err := natDatagram.ReadFrom(make([]byte, 100)); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Errorf("reading a 1200 byte datagram into a 100 byte buffer: %v, want spec.ErrMessageTooLong", err)
	}
}

// A write that fails must not count as activity, or a socket whose every send
// fails would outlive its idle timeout.
func TestAFailedWriteDoesNotKeepASocketAlive(t *testing.T) {
	failing := &failingDatagram{}
	dial := func(context.Context) (spec.Datagram, error) { return failing, nil }

	natDatagram := newNAT(t, dial, mustPolicy(nat.EndpointIndependentMapping, nat.EndpointIndependentFiltering, 50*time.Millisecond))
	defer natDatagram.Close()

	// Written to far more often than the idle timeout, so marking activity
	// before the write rather than after would keep this socket alive forever.
	_, destinationAddr := destination(t, "127.0.0.1")
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := natDatagram.WriteTo([]byte("x"), destinationAddr); err == nil {
			t.Fatal("the failing socket accepted a write")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !failing.wasClosed() {
		t.Error("a socket that never sent anything successfully survived its idle timeout")
	}
}

type failingDatagram struct {
	mu     sync.Mutex
	closed bool
}

func (d *failingDatagram) WriteTo([]byte, spec.Destination) (int, error) {
	return 0, errors.New("write refused")
}

func (d *failingDatagram) ReadFrom([]byte) (int, spec.Destination, error) {
	<-make(chan struct{}) // blocks until Close, which is all this needs to do
	return 0, spec.Destination{}, nil
}

func (d *failingDatagram) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

func (d *failingDatagram) LocalAddr() net.Addr { return nil }

func (d *failingDatagram) wasClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func TestWriteToAfterCloseReturnsErrClosed(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, nat.NewDefaultPolicy())

	_, destinationAddr := destination(t, "127.0.0.1")
	if _, err := natDatagram.WriteTo([]byte("before"), destinationAddr); err != nil {
		t.Fatalf("WriteTo before Close: %v", err)
	}
	natDatagram.Close()

	// A nil map would panic here rather than return; the closed check is what
	// makes this an error.
	if _, err := natDatagram.WriteTo([]byte("after"), destinationAddr); !errors.Is(err, nat.ErrClosed) {
		t.Errorf("WriteTo after Close: %v, want nat.ErrClosed", err)
	}
}

// A destination never written to before Close still takes the closed path,
// which is the one that has to close the socket it just dialed.
func TestWriteToAfterCloseOnAFreshKeyReturnsErrClosed(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, nat.NewDefaultPolicy())
	natDatagram.Close()

	_, destinationAddr := destination(t, "127.0.0.1")
	if _, err := natDatagram.WriteTo([]byte("after"), destinationAddr); !errors.Is(err, nat.ErrClosed) {
		t.Errorf("WriteTo after Close: %v, want nat.ErrClosed", err)
	}
}

// Racing first writes to one key may dial more than once, but only one socket
// may survive: the losers must be closed, not leaked.
func TestConcurrentFirstWritesLeaveOneSocket(t *testing.T) {
	real, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}

	var (
		mu     sync.Mutex
		opened []*trackedDatagram
	)
	dial := func(ctx context.Context) (spec.Datagram, error) {
		inner, err := real.CreateDatagram(ctx, spec.Endpoint{})
		if err != nil {
			return nil, err
		}
		tracked := &trackedDatagram{Datagram: inner}
		mu.Lock()
		opened = append(opened, tracked)
		mu.Unlock()
		return tracked, nil
	}

	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.EndpointIndependentFiltering))
	defer natDatagram.Close()

	_, destinationAddr := destination(t, "127.0.0.1")

	var writers sync.WaitGroup
	for range 16 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			natDatagram.WriteTo([]byte("x"), destinationAddr)
		}()
	}
	writers.Wait()

	mu.Lock()
	defer mu.Unlock()
	alive := 0
	for _, tracked := range opened {
		if !tracked.wasClosed() {
			alive++
		}
	}
	if alive != 1 {
		t.Errorf("%d of %d dialed sockets survived, want exactly 1", alive, len(opened))
	}
}

type trackedDatagram struct {
	spec.Datagram

	mu     sync.Mutex
	closed bool
}

func (d *trackedDatagram) Close() error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	return d.Datagram.Close()
}

func (d *trackedDatagram) wasClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// --- D9: domain destinations --------------------------------------------------

func domainDestination(t *testing.T, domain string, port uint16) spec.Destination {
	t.Helper()

	d, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		t.Fatalf("NewDestinationFromDomain: %v", err)
	}
	return d
}

// The reply to a domain destination arrives from the resolved IP. Under the
// default filtering it used to match nothing and be dropped; now it is matched
// against what the write actually went to, and reported back under the domain
// the caller asked for.
func TestDomainRepliesAreAcceptedAndReportedAsTheDomain(t *testing.T) {
	for _, filtering := range []nat.FilteringBehavior{
		nat.AddressAndPortDependentFiltering,
		nat.AddressDependentFiltering,
	} {
		t.Run(string(filtering), func(t *testing.T) {
			dial, _ := countingDialer(t)
			natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, filtering))
			defer natDatagram.Close()

			peer, peerAddr := destination(t, "127.0.0.1")
			target := domainDestination(t, "localhost", peerAddr.Port())

			if _, err := natDatagram.WriteTo([]byte("q"), target); err != nil {
				t.Fatalf("WriteTo a domain: %v", err)
			}

			source := sourceSeenBy(t, peer)
			mustSend(t, peer, "reply", source)

			arrivals := startReading(t, natDatagram)
			from := expectArrival(t, arrivals, shortWait)

			domain, isDomain := from.Domain()
			if !isDomain {
				t.Fatalf("reply reported as %s, want the domain the client sent to", from)
			}
			if domain != "localhost" || from.Port() != peerAddr.Port() {
				t.Errorf("reply reported as %s, want localhost:%d", from, peerAddr.Port())
			}
		})
	}
}

// Accepting a domain's real address must not accept everyone else's.
func TestAnUnrelatedSenderIsStillDroppedForADomainDestination(t *testing.T) {
	dial, _ := countingDialer(t)
	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering))
	defer natDatagram.Close()

	peer, peerAddr := destination(t, "127.0.0.1")
	if _, err := natDatagram.WriteTo([]byte("q"), domainDestination(t, "localhost", peerAddr.Port())); err != nil {
		t.Fatalf("WriteTo a domain: %v", err)
	}

	natAddr := sourceSeenBy(t, peer)
	stranger, _ := destination(t, "127.0.0.1")

	arrivals := startReading(t, natDatagram)
	mustSend(t, stranger, "spoofed", natAddr)
	expectNothing(t, arrivals, shortWait)
}

// A domain can resolve to several addresses, and the answer can change between
// writes, so what was sent to is accumulated rather than replaced.
func TestEveryResolvedAddressIsAccepted(t *testing.T) {
	first := netip.MustParseAddr("192.0.2.10")
	second := netip.MustParseAddr("192.0.2.11")

	carrier := &scriptedResolver{sentTo: []netip.Addr{first, second}, replies: make(chan spec.Destination, 4)}
	dial := func(context.Context) (spec.Datagram, error) { return carrier, nil }

	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering))
	defer natDatagram.Close()

	target := domainDestination(t, "many.example", 53)
	for range 2 {
		if _, err := natDatagram.WriteTo([]byte("q"), target); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
	}

	arrivals := startReading(t, natDatagram)
	for _, address := range []netip.Addr{first, second} {
		replyFrom, err := spec.NewDestinationFromIP(address, 53)
		if err != nil {
			t.Fatalf("NewDestinationFromIP: %v", err)
		}
		carrier.replies <- replyFrom

		from := expectArrival(t, arrivals, shortWait)
		if domain, ok := from.Domain(); !ok || domain != "many.example" {
			t.Errorf("reply from %s reported as %s, want many.example:53", address, from)
		}
	}
}

// scriptedResolver is a carrier that resolves: it hands out a different
// address per write, which is what a domain with several A records does.
type scriptedResolver struct {
	mu     sync.Mutex
	sentTo []netip.Addr
	writes int

	replies chan spec.Destination
	closed  chan struct{}
	once    sync.Once
}

func (c *scriptedResolver) WriteTo(p []byte, to spec.Destination) (int, error) {
	n, _, err := c.WriteToResolved(p, to)
	return n, err
}

func (c *scriptedResolver) WriteToResolved(p []byte, to spec.Destination) (int, spec.Endpoint, error) {
	c.mu.Lock()
	address := c.sentTo[min(c.writes, len(c.sentTo)-1)]
	c.writes++
	c.mu.Unlock()

	sentTo, err := spec.NewEndpoint(address, to.Port())
	if err != nil {
		return 0, spec.Endpoint{}, err
	}
	return len(p), sentTo, nil
}

func (c *scriptedResolver) ReadFrom(p []byte) (int, spec.Destination, error) {
	c.init()
	select {
	case from := <-c.replies:
		return copy(p, []byte("reply")), from, nil
	case <-c.closed:
		return 0, spec.Destination{}, errors.New("scripted: closed")
	}
}

func (c *scriptedResolver) Close() error {
	c.init()
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *scriptedResolver) LocalAddr() net.Addr { return nil }

func (c *scriptedResolver) init() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed == nil {
		c.closed = make(chan struct{})
	}
}

func requireArrival(t *testing.T, arrivals <-chan spec.Destination, wait time.Duration) spec.Destination {
	t.Helper()

	arrived := expectArrival(t, arrivals, wait)
	if arrived == nil {
		t.Fatal("no reply arrived")
	}
	return *arrived
}

// Two domains behind one CDN address. A reply must be reported under the
// domain actually contacted on that ip:port, never the other one.
func TestSharedAddressReportsTheDomainContactedOnThatPort(t *testing.T) {
	shared := netip.MustParseAddr("192.0.2.50")

	carrier := &scriptedResolver{sentTo: []netip.Addr{shared, shared}, replies: make(chan spec.Destination, 4)}
	dial := func(context.Context) (spec.Datagram, error) { return carrier, nil }

	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering))
	defer natDatagram.Close()

	first := domainDestination(t, "first.example", 443)
	second := domainDestination(t, "second.example", 8443)
	for _, target := range []spec.Destination{first, second} {
		if _, err := natDatagram.WriteTo([]byte("q"), target); err != nil {
			t.Fatalf("WriteTo %s: %v", target, err)
		}
	}

	arrivals := startReading(t, natDatagram)

	for _, expected := range []spec.Destination{first, second} {
		replyFrom, err := spec.NewDestinationFromIP(shared, expected.Port())
		if err != nil {
			t.Fatalf("NewDestinationFromIP: %v", err)
		}
		carrier.replies <- replyFrom

		from := requireArrival(t, arrivals, shortWait)
		if from != expected {
			t.Errorf("reply from %s:%d reported as %s, want %s", shared, expected.Port(), from, expected)
		}
	}
}

// Two domains on one address AND one port is genuinely ambiguous, so the
// sender is reported unchanged rather than guessed at.
func TestAnAmbiguousSharedAddressIsNotTranslated(t *testing.T) {
	shared := netip.MustParseAddr("192.0.2.51")

	carrier := &scriptedResolver{sentTo: []netip.Addr{shared, shared}, replies: make(chan spec.Destination, 4)}
	dial := func(context.Context) (spec.Datagram, error) { return carrier, nil }

	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering))
	defer natDatagram.Close()

	for _, domain := range []string{"first.example", "second.example"} {
		if _, err := natDatagram.WriteTo([]byte("q"), domainDestination(t, domain, 443)); err != nil {
			t.Fatalf("WriteTo %s: %v", domain, err)
		}
	}

	arrivals := startReading(t, natDatagram)
	replyFrom, err := spec.NewDestinationFromIP(shared, 443)
	if err != nil {
		t.Fatalf("NewDestinationFromIP: %v", err)
	}
	carrier.replies <- replyFrom

	from := requireArrival(t, arrivals, shortWait)
	if _, isDomain := from.Domain(); isDomain {
		t.Errorf("reply reported as %s, want the literal sender: two domains share that address and port", from)
	}
}

// A destination we contacted as a literal IP must never be relabelled, even
// when a domain happens to resolve to the same address.
func TestALiteralDestinationIsNeverRelabelled(t *testing.T) {
	shared := netip.MustParseAddr("192.0.2.52")

	carrier := &scriptedResolver{sentTo: []netip.Addr{shared}, replies: make(chan spec.Destination, 4)}
	dial := func(context.Context) (spec.Datagram, error) { return carrier, nil }

	natDatagram := newNAT(t, dial, policy(nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering))
	defer natDatagram.Close()

	literal, err := spec.NewDestinationFromIP(shared, 443)
	if err != nil {
		t.Fatalf("NewDestinationFromIP: %v", err)
	}
	if _, err := natDatagram.WriteTo([]byte("q"), domainDestination(t, "cdn.example", 443)); err != nil {
		t.Fatalf("WriteTo the domain: %v", err)
	}
	if _, err := natDatagram.WriteTo([]byte("q"), literal); err != nil {
		t.Fatalf("WriteTo the literal: %v", err)
	}

	arrivals := startReading(t, natDatagram)
	carrier.replies <- literal

	if from := requireArrival(t, arrivals, shortWait); from != literal {
		t.Errorf("reply reported as %s, want the literal %s we contacted directly", from, literal)
	}
}

// A sprayer sending many packets that filtering rejects must not produce one
// log line per packet.
func TestASprayerIsLoggedOncePerInterval(t *testing.T) {
	sink := logging.NewMemorySink(64, logging.LevelDebug)
	system, err := logging.New(logging.Config{Level: logging.LevelDebug, Outputs: []logging.Output{logging.HandlerOutput{Handler: sink}}})
	if err != nil {
		t.Fatal(err)
	}
	dial, _ := countingDialer(t)
	natDatagram, err := nat.New(context.Background(), dial, policy(nat.EndpointIndependentMapping, nat.AddressAndPortDependentFiltering), system.Logger())
	if err != nil {
		t.Fatal(err)
	}
	defer natDatagram.Close()

	contacted, contactedAddr := destination(t, "127.0.0.1")
	sprayer, _ := destination(t, "127.0.0.2")
	arrivals := startReading(t, natDatagram)
	if _, err := natDatagram.WriteTo([]byte("hello"), contactedAddr); err != nil {
		t.Fatal(err)
	}
	tableAddr := sourceSeenBy(t, contacted)

	for range 20 {
		mustSend(t, sprayer, "spray", tableAddr)
	}
	expectNothing(t, arrivals, shortWait)

	notices := 0
	for _, record := range sink.Records() {
		if record.Message == "inbound datagram rejected by filtering policy" {
			notices++
		}
	}
	if notices != 1 {
		t.Errorf("%d rejection notices for 20 sprayed packets, want 1", notices)
	}
}
