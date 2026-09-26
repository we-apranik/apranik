package udp_test

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/module/transport/udp"
	"github.com/The-Apranik/Apranik/spec"
)

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
