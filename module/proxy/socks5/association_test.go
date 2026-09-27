package socks5

import (
	"net"
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

// endlessDatagram hands out the same packet from the same sender forever and
// discards writes, so a benchmark measures only the layer above it.
type endlessDatagram struct {
	packet []byte
	from   spec.Destination
}

func (d endlessDatagram) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	return copy(buffer, d.packet), d.from, nil
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
	packet, err := socksaddr.AppendAddress([]byte{0, 0, 0}, benchmarkDestination(b, "1.1.1.1", 53))
	if err != nil {
		b.Fatal(err)
	}
	packet = append(packet, make([]byte, 512)...)
	session := newAssociation(endlessDatagram{packet: packet, from: client}, spec.Destination{})
	payload := make([]byte, maxDatagram)

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := session.ReadFrom(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAssociationWriteTo(b *testing.B) {
	client := benchmarkDestination(b, "127.0.0.1", 40000)
	session := newAssociation(endlessDatagram{from: client}, client)
	source := benchmarkDestination(b, "1.1.1.1", 53)
	reply := make([]byte, 512)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := session.WriteTo(reply, source); err != nil {
			b.Fatal(err)
		}
	}
}
