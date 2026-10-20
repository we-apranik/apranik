package outbound_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/outbound"
	"github.com/we-apranik/apranik/spec"
)

func destination(t testing.TB, ip string, port uint16) spec.Destination {
	t.Helper()
	result, err := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type boundDatagram struct {
	destination spec.Destination
	written     chan []byte
	replies     chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
	writeError  error
}

func (d *boundDatagram) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	select {
	case reply := <-d.replies:
		if len(reply) > len(buffer) {
			return 0, spec.Destination{}, spec.ErrMessageTooLong
		}
		return copy(buffer, reply), d.destination, nil
	case <-d.closed:
		return 0, spec.Destination{}, net.ErrClosed
	}
}

func (d *boundDatagram) WriteTo(payload []byte, _ spec.Destination) (int, error) {
	select {
	case <-d.closed:
		return 0, net.ErrClosed
	default:
	}
	if d.writeError != nil {
		return 0, d.writeError
	}
	d.written <- append([]byte(nil), payload...)
	return len(payload), nil
}

func (d *boundDatagram) Close() error {
	d.closeOnce.Do(func() { close(d.closed) })
	return nil
}

func (*boundDatagram) LocalAddr() net.Addr { return &net.UDPAddr{} }

type streamRequestClient struct {
	mutex     sync.Mutex
	datagrams []*boundDatagram
	failWith  error
	placement spec.DatagramDestinationPlacement
}

func (c *streamRequestClient) DatagramDestinationIsWritten() spec.DatagramDestinationPlacement {
	if c.placement != 0 {
		return c.placement
	}
	return spec.DatagramDestinationInStreamRequest
}

func (c *streamRequestClient) RequestDatagramOverStream(_ context.Context, _ spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	if c.failWith != nil {
		return nil, c.failWith
	}
	datagram := &boundDatagram{
		destination: metadata.Destination,
		written:     make(chan []byte, 8),
		replies:     make(chan []byte, 8),
		closed:      make(chan struct{}),
	}
	c.mutex.Lock()
	c.datagrams = append(c.datagrams, datagram)
	c.mutex.Unlock()
	return datagram, nil
}

func (c *streamRequestClient) datagramFor(t *testing.T, destination spec.Destination) *boundDatagram {
	t.Helper()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, datagram := range c.datagrams {
		if datagram.destination == destination {
			return datagram
		}
	}
	t.Fatalf("no datagram was requested for %s", destination)
	return nil
}

func (c *streamRequestClient) count() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return len(c.datagrams)
}

type countingDialer struct {
	mutex sync.Mutex
	dials int
	err   error
}

func (d *countingDialer) DialStream(context.Context, spec.Destination) (spec.Stream, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	d.dials++
	ourEnd, serverEnd := net.Pipe()
	serverEnd.Close()
	return pipeStream{ourEnd}, nil
}

func (*countingDialer) Close() error { return nil }

func (d *countingDialer) count() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.dials
}

func newPerDestinationDatagram(t *testing.T, dialer *countingDialer, client *streamRequestClient) spec.Datagram {
	t.Helper()
	proxy := outbound.NewStream(outbound.StreamConfig{
		Tag: "proxy", ServerAddr: proxyServer(t), Dialer: dialer, DatagramOverStreamClient: client,
	})
	datagram, err := proxy.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { datagram.Close() })
	return datagram
}

func expectWritten(t *testing.T, datagram *boundDatagram, want string) {
	t.Helper()
	select {
	case got := <-datagram.written:
		if string(got) != want {
			t.Errorf("%s got %q, want %q", datagram.destination, got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s was never written %q", datagram.destination, want)
	}
}

func TestEachNewDestinationGetsItsOwnStreamAndAKnownOneKeepsIt(t *testing.T) {
	dialer, client := &countingDialer{}, &streamRequestClient{}
	datagram := newPerDestinationDatagram(t, dialer, client)
	dns, stun := destination(t, "1.1.1.1", 53), destination(t, "8.8.8.8", 3478)

	for _, step := range []struct {
		to      spec.Destination
		payload string
	}{{dns, "query"}, {stun, "binding"}, {dns, "again"}} {
		if _, err := datagram.WriteTo([]byte(step.payload), step.to); err != nil {
			t.Fatal(err)
		}
	}

	if dialer.count() != 2 || client.count() != 2 {
		t.Errorf("%d dials and %d requests, want one stream per destination: 2 and 2", dialer.count(), client.count())
	}
	expectWritten(t, client.datagramFor(t, dns), "query")
	expectWritten(t, client.datagramFor(t, dns), "again")
	expectWritten(t, client.datagramFor(t, stun), "binding")
}

func TestRepliesFromEveryDestinationComeBackThroughTheOneDatagram(t *testing.T) {
	client := &streamRequestClient{}
	datagram := newPerDestinationDatagram(t, &countingDialer{}, client)
	dns, stun := destination(t, "1.1.1.1", 53), destination(t, "8.8.8.8", 3478)
	datagram.WriteTo([]byte("q"), dns)
	datagram.WriteTo([]byte("b"), stun)

	client.datagramFor(t, dns).replies <- []byte("answer")
	client.datagramFor(t, stun).replies <- []byte("mapped")

	seen := map[spec.Destination]string{}
	buffer := make([]byte, 64)
	for range 2 {
		size, from, err := datagram.ReadFrom(buffer)
		if err != nil {
			t.Fatal(err)
		}
		seen[from] = string(buffer[:size])
	}
	if seen[dns] != "answer" || seen[stun] != "mapped" {
		t.Errorf("replies = %v, want each from its own destination", seen)
	}
}

func TestAReplyLargerThanTheBufferIsReportedAndTheNextOneStillArrives(t *testing.T) {
	client := &streamRequestClient{}
	datagram := newPerDestinationDatagram(t, &countingDialer{}, client)
	dns := destination(t, "1.1.1.1", 53)
	datagram.WriteTo([]byte("q"), dns)

	reply := client.datagramFor(t, dns).replies
	buffer := make([]byte, 4)
	reply <- []byte("too long for it")
	if _, _, err := datagram.ReadFrom(buffer); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Fatalf("error = %v, want spec.ErrMessageTooLong", err)
	}
	reply <- []byte("ok")
	if size, _, err := datagram.ReadFrom(buffer); err != nil || string(buffer[:size]) != "ok" {
		t.Errorf("next reply = %q, %v, want it intact", buffer[:size], err)
	}
}

func TestAFailedDialIsReportedAsOpeningTheWayToTheDestination(t *testing.T) {
	refusal := errors.New("connection refused")
	dialer := &countingDialer{err: refusal}
	datagram := newPerDestinationDatagram(t, dialer, &streamRequestClient{})

	_, err := datagram.WriteTo([]byte("q"), destination(t, "1.1.1.1", 53))
	if !errors.Is(err, component.ErrOpeningDatagramForDestination) || !errors.Is(err, refusal) {
		t.Errorf("error = %v, want one wrapping both the marker and the dial error", err)
	}
}

func TestARefusedRequestIsReportedAsOpeningTheWayAndLeavesNothingBehind(t *testing.T) {
	refusal := errors.New("bad uuid")
	datagram := newPerDestinationDatagram(t, &countingDialer{}, &streamRequestClient{failWith: refusal})

	_, err := datagram.WriteTo([]byte("q"), destination(t, "1.1.1.1", 53))
	if !errors.Is(err, component.ErrOpeningDatagramForDestination) || !errors.Is(err, refusal) {
		t.Errorf("error = %v, want one wrapping both the marker and the client's error", err)
	}
}

func TestADestinationWhoseStreamDiedIsOpenedAgainOnTheNextWrite(t *testing.T) {
	dialer, client := &countingDialer{}, &streamRequestClient{}
	datagram := newPerDestinationDatagram(t, dialer, client)
	dns := destination(t, "1.1.1.1", 53)
	datagram.WriteTo([]byte("first"), dns)

	client.datagramFor(t, dns).Close()
	if _, err := datagram.WriteTo([]byte("lost"), dns); err == nil {
		t.Fatal("a write to a closed stream reported no error")
	}
	if _, err := datagram.WriteTo([]byte("again"), dns); err != nil {
		t.Fatalf("the destination was not opened again: %v", err)
	}
	if dialer.count() != 2 {
		t.Errorf("%d dials, want the destination opened again after its stream died", dialer.count())
	}
}

func TestADestinationWhoseStreamDiedWhileIdleIsForgottenByItsReplyLoop(t *testing.T) {
	dialer, client := &countingDialer{}, &streamRequestClient{}
	datagram := newPerDestinationDatagram(t, dialer, client)
	go func() {
		buffer := make([]byte, 64)
		for {
			if _, _, err := datagram.ReadFrom(buffer); err != nil {
				return
			}
		}
	}()
	dns := destination(t, "1.1.1.1", 53)
	datagram.WriteTo([]byte("first"), dns)

	client.datagramFor(t, dns).Close()
	deadline := time.Now().Add(2 * time.Second)
	for dialer.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		datagram.WriteTo([]byte("again"), dns)
	}
	if dialer.count() < 2 {
		t.Fatalf("%d dials, want the destination opened again once its reply loop saw it die", dialer.count())
	}
}

func TestAWriteThatFailsDropsOnlyThatDestinationsDatagram(t *testing.T) {
	dialer, client := &countingDialer{}, &streamRequestClient{}
	datagram := newPerDestinationDatagram(t, dialer, client)
	dns, stun := destination(t, "1.1.1.1", 53), destination(t, "8.8.8.8", 3478)
	datagram.WriteTo([]byte("q"), dns)
	datagram.WriteTo([]byte("b"), stun)

	client.datagramFor(t, dns).writeError = errors.New("broken pipe")
	if _, err := datagram.WriteTo([]byte("q2"), dns); err == nil {
		t.Fatal("the failed write reported no error")
	}
	if _, err := datagram.WriteTo([]byte("b2"), stun); err != nil {
		t.Errorf("the other destination failed too: %v", err)
	}
	if dialer.count() != 2 {
		t.Errorf("%d dials, want the other destination's stream untouched", dialer.count())
	}
}

func TestClosingTheDatagramClosesEveryDestinationsDatagram(t *testing.T) {
	client := &streamRequestClient{}
	datagram := newPerDestinationDatagram(t, &countingDialer{}, client)
	dns, stun := destination(t, "1.1.1.1", 53), destination(t, "8.8.8.8", 3478)
	datagram.WriteTo([]byte("q"), dns)
	datagram.WriteTo([]byte("b"), stun)

	datagram.Close()
	for _, to := range []spec.Destination{dns, stun} {
		select {
		case <-client.datagramFor(t, to).closed:
		case <-time.After(2 * time.Second):
			t.Errorf("the datagram for %s was left open", to)
		}
	}
	if _, err := datagram.WriteTo([]byte("late"), dns); !errors.Is(err, net.ErrClosed) {
		t.Errorf("write after Close = %v, want net.ErrClosed", err)
	}
	if _, _, err := datagram.ReadFrom(make([]byte, 8)); !errors.Is(err, net.ErrClosed) {
		t.Errorf("read after Close = %v, want net.ErrClosed", err)
	}
}

func TestAClientThatNamesEachDatagramsDestinationKeepsTheOneStream(t *testing.T) {
	dialer := &countingDialer{}
	client := &streamRequestClient{placement: spec.DatagramDestinationInEveryDatagram}
	proxy := outbound.NewStream(outbound.StreamConfig{
		Tag: "proxy", ServerAddr: proxyServer(t), Dialer: dialer, DatagramOverStreamClient: client,
	})
	datagram, err := proxy.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	defer datagram.Close()

	if _, wrapped := datagram.(*boundDatagram); !wrapped {
		t.Errorf("CreateDatagram returned %T, want the client's own datagram on one stream", datagram)
	}
	if dialer.count() != 1 {
		t.Errorf("%d dials, want one stream for the whole session", dialer.count())
	}
}

func TestAClientThatSaysNothingAboutWhereItWritesTheDestinationIsRefused(t *testing.T) {
	client := &streamRequestClient{placement: 99}
	proxy := outbound.NewStream(outbound.StreamConfig{
		Tag: "proxy", ServerAddr: proxyServer(t), Dialer: &countingDialer{}, DatagramOverStreamClient: client,
	})
	if _, err := proxy.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP}); err == nil {
		t.Error("an unknown placement was accepted")
	}
}
