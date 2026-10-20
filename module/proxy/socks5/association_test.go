package socks5

import (
	"net"
	"net/netip"
	"testing"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/socksaddr"
	"github.com/we-apranik/apranik/spec"
)

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
	udpAssociation := newAssociation(endlessDatagram{datagram: datagram, from: client}, spec.Destination{}, defaultMaxDatagramSize)
	payload := make([]byte, defaultMaxDatagramSize)

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := udpAssociation.ReadFrom(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAssociationWriteTo(b *testing.B) {
	client := benchmarkDestination(b, "127.0.0.1", 40000)
	udpAssociation := newAssociation(endlessDatagram{from: client}, client, defaultMaxDatagramSize)
	source := benchmarkDestination(b, "1.1.1.1", 53)
	reply := make([]byte, 512)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := udpAssociation.WriteTo(reply, source); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAnAssociationsBufferIsTheLargestPayloadPlusTheLargestHeader(t *testing.T) {
	udpAssociation := newAssociation(endlessDatagram{}, spec.Destination{}, 1000)
	if got, want := len(udpAssociation.readBuffer), 1000+largestUDPHeader; got != want {
		t.Errorf("buffer = %d bytes, want %d", got, want)
	}
}

func TestTheUDPDatagramSizeSettingDefaultsAndIsChecked(t *testing.T) {
	settings := Settings{UDPRelay: true}
	if got := settings.maxDatagramSize(); got != defaultMaxDatagramSize {
		t.Errorf("default = %d, want %d", got, defaultMaxDatagramSize)
	}
	settings.UDPMaxDatagramSize = types.Pointer(1500)
	if settings.maxDatagramSize() != 1500 || settings.Validate() != nil {
		t.Error("1500 was not accepted as it is")
	}
	for _, refused := range []int{-1, 0, largestDatagramSize + 1} {
		settings.UDPMaxDatagramSize = types.Pointer(refused)
		if settings.Validate() == nil {
			t.Errorf("udp_max_datagram_size %d was accepted", refused)
		}
	}
}
