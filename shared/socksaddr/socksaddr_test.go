package socksaddr_test

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"testing"

	"github.com/we-apranik/apranik/shared/socksaddr"
	"github.com/we-apranik/apranik/spec"
)

func destinations(t *testing.T) []spec.Destination {
	t.Helper()
	ipv4, _ := spec.NewDestinationFromIP(netip.MustParseAddr("1.2.3.4"), 53)
	ipv6, _ := spec.NewDestinationFromIP(netip.MustParseAddr("2001:db8::1"), 443)
	domain, err := spec.NewDestinationFromDomain("example.com", 8080)
	if err != nil {
		t.Fatal(err)
	}
	return []spec.Destination{ipv4, ipv6, domain}
}

func TestParseAddressMatchesReadAddress(t *testing.T) {
	for _, want := range destinations(t) {
		encoded, err := socksaddr.AppendAddress(nil, want)
		if err != nil {
			t.Fatal(err)
		}
		withTrailer := append(bytes.Clone(encoded), "payload"...)

		parsed, length, err := socksaddr.ParseAddress(withTrailer)
		if err != nil || parsed != want || length != len(encoded) {
			t.Errorf("ParseAddress(%s) = %s, %d, %v; want %s, %d", want, parsed, length, err, want, len(encoded))
		}
		read, err := socksaddr.ReadAddress(bytes.NewReader(encoded))
		if err != nil || read != parsed {
			t.Errorf("ReadAddress(%s) = %s, %v; ParseAddress gave %s", want, read, err, parsed)
		}
	}
}

func TestParseAddressRefusesShortAndMalformedInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		encoded []byte
		want    error
	}{
		{"empty", nil, io.ErrUnexpectedEOF},
		{"short IPv4", []byte{socksaddr.AddressIPv4, 1, 2, 3, 4, 0}, io.ErrUnexpectedEOF},
		{"short IPv6", []byte{socksaddr.AddressIPv6, 1, 2, 3}, io.ErrUnexpectedEOF},
		{"no domain length", []byte{socksaddr.AddressDomain}, io.ErrUnexpectedEOF},
		{"short domain", []byte{socksaddr.AddressDomain, 5, 'a', 'b', 0, 53}, io.ErrUnexpectedEOF},
		{"empty domain", []byte{socksaddr.AddressDomain, 0, 0, 53}, socksaddr.ErrEmptyDomain},
		{"unknown type", []byte{0x09, 0, 0}, socksaddr.ErrUnsupportedAddressType},
	} {
		if _, _, err := socksaddr.ParseAddress(tc.encoded); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestParseAddressUnmapsIPv4InIPv6(t *testing.T) {
	mapped := netip.MustParseAddr("::ffff:1.2.3.4").As16()
	encoded := append(append([]byte{socksaddr.AddressIPv6}, mapped[:]...), 0, 53)
	parsed, _, err := socksaddr.ParseAddress(encoded)
	if ip, _ := parsed.IP(); err != nil || !ip.Is4() {
		t.Errorf("ParseAddress = %s, %v; want an IPv4 address", parsed, err)
	}
}

func BenchmarkReadAddressIPv4(b *testing.B) {
	destination, _ := spec.NewDestinationFromIP(netip.MustParseAddr("1.1.1.1"), 53)
	encoded, err := socksaddr.AppendAddress(nil, destination)
	if err != nil {
		b.Fatal(err)
	}
	reader := bytes.NewReader(encoded)

	b.ReportAllocs()
	for b.Loop() {
		reader.Reset(encoded)
		if _, err := socksaddr.ReadAddress(reader); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseAddressIPv4(b *testing.B) {
	destination, _ := spec.NewDestinationFromIP(netip.MustParseAddr("1.1.1.1"), 53)
	encoded, err := socksaddr.AppendAddress(nil, destination)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := socksaddr.ParseAddress(encoded); err != nil {
			b.Fatal(err)
		}
	}
}
