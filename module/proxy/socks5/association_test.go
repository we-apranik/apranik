package socks5

import (
	"net"
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

// endlessDatagram hands out the same datagram from the same sender forever and
// discards writes, so a benchmark measures only the layer above it.
type endlessDatagram struct {
	datagram []byte
	from     spec.Destination
}

func (d endlessDatagram) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	return copy(buffer, d.datagram), d.from, nil
}
func (endlessDatagram) WriteTo(payload []byte, _ spec.Destination) (int, error) {
	return len(payload), nil
}
func (endlessDatagram) Close() error        { return nil }
func (endlessDatagram) LocalAddr() net.Addr { return nil }

func benchmarkDestination(b *testing.B, ip string, port uint16) spec.Destination {
	b.Helper()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
	if err != nil {
		b.Fatal(err)
	}
	return destination
}

func BenchmarkAssociationReadFrom(b *testing.B) {
	client := benchmarkDestination(b, "127.0.0.1", 40000)
	datagram, err := socksaddr.AppendAddress([]byte{0, 0, 0}, benchmarkDestination(b, "1.1.1.1", 53))
	if err != nil {
		b.Fatal(err)
	}
	datagram = append(datagram, make([]byte, 512)...)
	udpAssociation := newAssociation(endlessDatagram{datagram: datagram, from: client}, spec.Destination{})
	payload := make([]byte, maxDatagramSize)

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := udpAssociation.ReadFrom(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAssociationWriteTo(b *testing.B) {
	client := benchmarkDestination(b, "127.0.0.1", 40000)
	udpAssociation := newAssociation(endlessDatagram{from: client}, client)
	source := benchmarkDestination(b, "1.1.1.1", 53)
	reply := make([]byte, 512)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := udpAssociation.WriteTo(reply, source); err != nil {
			b.Fatal(err)
		}
	}
}
