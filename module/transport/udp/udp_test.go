package udp_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/transport/udp"
	"github.com/The-Apranik/Apranik/spec"
)

func endpoint(ip string, port uint16) spec.Endpoint {
	e, err := spec.NewEndpoint(netip.MustParseAddr(ip), port)
	if err != nil {
		panic(err)
	}
	return e
}

func domainDst(domain string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		panic(err)
	}
	return destination
}

func bindLocal(t *testing.T) (spec.Datagram, spec.Destination) {
	t.Helper()
	return bindLocalOn(t, netip.MustParseAddr("127.0.0.1"))
}

func bindLocalOn(t *testing.T, ip netip.Addr) (spec.Datagram, spec.Destination) {
	t.Helper()

	ln, err := (&udp.Transport{}).NewDatagramListener(endpoint(ip.String(), 0))
	if err != nil {
		t.Fatalf("NewDatagramListener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	c, err := ln.ListenDatagram(context.Background())
	if err != nil {
		t.Fatalf("ListenDatagram: %v", err)
	}

	addr, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("LocalAddr is %T, want *net.UDPAddr", c.LocalAddr())
	}
	return c, spec.NewEndpointFromNetAddr(addr).Destination()
}

func TestDatagramsTravelBothWays(t *testing.T) {
	server, serverAddr := bindLocal(t)

	d, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	client, err := d.CreateDatagram(context.Background(), spec.Endpoint{})
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer client.Close()

	if _, err := client.WriteTo([]byte("up"), serverAddr); err != nil {
		t.Fatalf("client write: %v", err)
	}

	buf := make([]byte, 64)
	n, from, err := server.ReadFrom(buf)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(buf[:n]) != "up" {
		t.Errorf("server read %q, want %q", buf[:n], "up")
	}

	if _, err := server.WriteTo([]byte("down"), from); err != nil {
		t.Fatalf("server write: %v", err)
	}
	n, _, err = client.ReadFrom(buf)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf[:n]) != "down" {
		t.Errorf("client read %q, want %q", buf[:n], "down")
	}
}

// A domain reaches the base transport unresolved and is resolved only here.
func TestWriteToADomain(t *testing.T) {
	server, serverAddr := bindLocal(t)

	d, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	client, err := d.CreateDatagram(context.Background(), spec.Endpoint{})
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer client.Close()

	// Twice, so the second write goes through the resolve cache.
	for i := range 2 {
		if _, err := client.WriteTo([]byte("hello"), domainDst("localhost", serverAddr.Port())); err != nil {
			t.Fatalf("write %d to a domain: %v", i, err)
		}
		buf := make([]byte, 64)
		n, _, err := server.ReadFrom(buf)
		if err != nil {
			t.Fatalf("server read %d: %v", i, err)
		}
		if string(buf[:n]) != "hello" {
			t.Errorf("server read %q, want %q", buf[:n], "hello")
		}
	}
}

func TestWriteToRejectsMalformedAddresses(t *testing.T) {
	c, _ := bindLocal(t)

	for _, tc := range []struct {
		name string
		to   spec.Destination
		want string
	}{
		// "both domain and IP" is gone from this table: spec.Destination's
		// constructors make it unrepresentable.
		{"neither domain nor IP", spec.Destination{}, "neither domain nor IP"},
		{"no port", domainDst("localhost", 0), "no port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.WriteTo([]byte("x"), tc.to); err == nil {
				t.Fatalf("WriteTo(%+v) succeeded, want an error", tc.to)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error is %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// One datagram, both address families. Picking a family from a destination
// used to pin the socket to whichever peer came first, so a session that sent
// to IPv4 could never afterwards reach IPv6 -- DNS then QUIC, in practice.
func TestOneDatagramReachesBothAddressFamilies(t *testing.T) {
	v4, v4Addr := bindLocal(t)
	v6, v6Addr := bindLocalOn(t, netip.MustParseAddr("::1"))

	d, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	client, err := d.CreateDatagram(context.Background(), spec.Endpoint{})
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer client.Close()

	for _, tc := range []struct {
		name   string
		to     spec.Destination
		server spec.Datagram
	}{
		{"IPv4 first", v4Addr, v4},
		{"then IPv6 on the same datagram", v6Addr, v6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := client.WriteTo([]byte("x"), tc.to); err != nil {
				t.Fatalf("write to %s: %v", tc.name, err)
			}
			buf := make([]byte, 8)
			n, _, err := tc.server.ReadFrom(buf)
			if err != nil {
				t.Fatalf("server read: %v", err)
			}
			if string(buf[:n]) != "x" {
				t.Errorf("server read %q, want %q", buf[:n], "x")
			}
		})
	}
}

// A caller that cares which interface traffic leaves by passes a local
// address; the usual caller passes a zero one and gets an ephemeral port.
func TestCreateDatagramBindsWhereTheCallerAsks(t *testing.T) {
	d, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}

	datagram, err := d.CreateDatagram(context.Background(), endpoint("127.0.0.1", 0))
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer datagram.Close()

	local, ok := datagram.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("LocalAddr is %T, want *net.UDPAddr", datagram.LocalAddr())
	}
	if !local.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("bound to %s, want the requested 127.0.0.1", local.IP)
	}
	if local.Port == 0 {
		t.Error("port 0 after binding, want the port the kernel chose")
	}
}

func TestModuleFillsOneKindOnly(t *testing.T) {
	var module any = &udp.Transport{}

	if _, ok := module.(spec.BaseDatagramTransportModule); !ok {
		t.Error("udp.Transport does not implement spec.BaseDatagramTransportModule")
	}
	if _, ok := module.(spec.OverlayDatagramTransportModule); ok {
		t.Error("udp.Transport implements spec.OverlayDatagramTransportModule; it is a base transport")
	}
	if _, ok := module.(spec.BaseStreamTransportModule); ok {
		t.Error("udp.Transport implements spec.BaseStreamTransportModule")
	}
}

func TestInfoIsConstant(t *testing.T) {
	if first, second := (&udp.Transport{}).Info(), udp.NewBaseDatagramTransportModule().Info(); first != second {
		t.Errorf("Info varies between instances: %+v then %+v", first, second)
	}
}

func TestBadBindAddressIsRefusedWhileBuilding(t *testing.T) {
	if _, err := (&udp.Transport{}).NewDatagramListener(spec.Endpoint{}); err == nil {
		t.Error("NewDatagramListener with neither domain nor IP succeeded, want an error")
	}
}

// Port 0 is what a SOCKS5 associate socket asks for. Nothing is bound until
// ListenDatagram, so there is no address to report before it.
func TestBindAcceptsAnEphemeralPort(t *testing.T) {
	listener, err := (&udp.Transport{}).NewDatagramListener(endpoint("127.0.0.1", 0))
	if err != nil {
		t.Fatalf("NewDatagramListener: %v", err)
	}
	defer listener.Close()

	if listener.LocalAddr() != nil {
		t.Errorf("LocalAddr is %v before ListenDatagram, want nil: nothing is bound yet", listener.LocalAddr())
	}

	if _, err := listener.ListenDatagram(context.Background()); err != nil {
		t.Fatalf("ListenDatagram: %v", err)
	}

	bound, ok := listener.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("LocalAddr is %T, want *net.UDPAddr", listener.LocalAddr())
	}
	if bound.Port == 0 {
		t.Error("port 0 after binding, want the port the kernel chose")
	}
}

// Two datagrams would share one connection, so either one's Close would take
// the other down with it.
func TestListenDatagramHandsOutOneDatagram(t *testing.T) {
	listener, err := (&udp.Transport{}).NewDatagramListener(endpoint("127.0.0.1", 0))
	if err != nil {
		t.Fatalf("NewDatagramListener: %v", err)
	}
	defer listener.Close()

	if _, err := listener.ListenDatagram(context.Background()); err != nil {
		t.Fatalf("first ListenDatagram: %v", err)
	}
	if _, err := listener.ListenDatagram(context.Background()); err == nil {
		t.Error("a second ListenDatagram succeeded, want it refused")
	}
}

// A cancelled context must not leave a socket open behind it.
func TestCreateDatagramHonoursACancelledContext(t *testing.T) {
	d, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := d.CreateDatagram(ctx, spec.Endpoint{}); err == nil {
		t.Error("CreateDatagram ignored a cancelled context")
	}
}

// A datagram larger than the caller's buffer would otherwise be truncated with
// no error at all, losing the tail silently.
func TestReadFromReportsATruncatedDatagram(t *testing.T) {
	server, serverAddr := bindLocal(t)

	d, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	client, err := d.CreateDatagram(context.Background(), spec.Endpoint{})
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer client.Close()

	if _, err := client.WriteTo(make([]byte, 1500), serverAddr); err != nil {
		t.Fatalf("client write: %v", err)
	}

	if _, _, err := server.ReadFrom(make([]byte, 100)); !errors.Is(err, spec.ErrMessageTooLong) {
		t.Errorf("reading a 1500 byte datagram into a 100 byte buffer: %v, want spec.ErrMessageTooLong", err)
	}
}

// The dialer's socket is the layer that resolves, so it must be able to say
// what it resolved to: that is the only way a NAT above it can match a reply,
// which always arrives from an IP, against a domain destination.
func TestSocketReportsWhatItResolvedTo(t *testing.T) {
	dialer, err := (&udp.Transport{}).NewDatagramDialer()
	if err != nil {
		t.Fatalf("NewDatagramDialer: %v", err)
	}
	client, err := dialer.CreateDatagram(context.Background(), spec.Endpoint{})
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer client.Close()

	resolving, ok := client.(spec.ResolvedWriter)
	if !ok {
		t.Fatalf("%T does not implement spec.ResolvedWriter", client)
	}

	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer server.Close()
	port := uint16(server.LocalAddr().(*net.UDPAddr).Port)

	_, sentTo, err := resolving.WriteToResolved([]byte("hello"), domainDst("localhost", port))
	if err != nil {
		t.Fatalf("WriteToResolved to a domain: %v", err)
	}
	if !sentTo.IsValid() {
		t.Fatal("WriteToResolved reported no address for a domain destination")
	}
	if !sentTo.Addr().IsLoopback() || sentTo.Port() != port {
		t.Errorf("resolved to %s, want loopback:%d", sentTo, port)
	}
}

func benchmarkSocketAndPeer(b *testing.B) (spec.Datagram, *net.UDPConn) {
	b.Helper()
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { peer.Close() })

	dialer, err := udp.NewBaseDatagramTransportModule().NewDatagramDialer()
	if err != nil {
		b.Fatal(err)
	}
	socket, err := dialer.CreateDatagram(context.Background(), spec.Endpoint{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { socket.Close() })
	return socket, peer
}

func drain(peer *net.UDPConn) {
	buffer := make([]byte, 2048)
	for {
		if _, _, err := peer.ReadFromUDPAddrPort(buffer); err != nil {
			return
		}
	}
}

func BenchmarkSocketWriteToIP(b *testing.B) {
	socket, peer := benchmarkSocketAndPeer(b)
	go drain(peer)
	to, _ := spec.NewDestinationFromIP(netip.MustParseAddr("127.0.0.1"), uint16(peer.LocalAddr().(*net.UDPAddr).Port))
	payload := make([]byte, 512)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := socket.WriteTo(payload, to); err != nil {
			b.Fatal(err)
		}
	}
}

// A domain the socket has already resolved: the cached path every packet
// after the first takes.
func BenchmarkSocketWriteToCachedDomain(b *testing.B) {
	socket, peer := benchmarkSocketAndPeer(b)
	go drain(peer)
	to, _ := spec.NewDestinationFromDomain("localhost", uint16(peer.LocalAddr().(*net.UDPAddr).Port))
	payload := make([]byte, 512)
	if _, err := socket.WriteTo(payload, to); err != nil {
		b.Skipf("localhost does not resolve here: %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := socket.WriteTo(payload, to); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSocketReadFrom(b *testing.B) {
	socket, peer := benchmarkSocketAndPeer(b)
	socketPort := socket.LocalAddr().(*net.UDPAddr).Port
	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: socketPort}
	payload := make([]byte, 512)
	buffer := make([]byte, 65507)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := peer.WriteToUDP(payload, target); err != nil {
			b.Fatal(err)
		}
		if _, _, err := socket.ReadFrom(buffer); err != nil {
			b.Fatal(err)
		}
	}
}
