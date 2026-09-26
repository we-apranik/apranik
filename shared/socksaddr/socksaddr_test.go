package socksaddr_test

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
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

// ParseAddress and ReadAddress must agree on every address, and ParseAddress
// must report exactly the bytes it took.
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

// An IPv4 address mapped into IPv6 is reported as IPv4, as ReadAddress does.
func TestParseAddressUnmapsIPv4InIPv6(t *testing.T) {
	mapped := netip.MustParseAddr("::ffff:1.2.3.4").As16()
	encoded := append(append([]byte{socksaddr.AddressIPv6}, mapped[:]...), 0, 53)
	parsed, _, err := socksaddr.ParseAddress(encoded)
	if ip, _ := parsed.IP(); err != nil || !ip.Is4() {
		t.Errorf("ParseAddress = %s, %v; want an IPv4 address", parsed, err)
	}
}
