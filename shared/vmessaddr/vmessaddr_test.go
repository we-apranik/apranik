package vmessaddr_test

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/shared/vmessaddr"
	"github.com/we-apranik/apranik/spec"
)

func ipDestination(t testing.TB, text string, port uint16) spec.Destination {
	t.Helper()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(text), port)
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func domainDestination(t testing.TB, domain string, port uint16) spec.Destination {
	t.Helper()
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func TestAppendAddressWritesThePortFirst(t *testing.T) {
	for _, tc := range []struct {
		name        string
		destination spec.Destination
		want        []byte
	}{
		{"IPv4", ipDestination(t, "1.2.3.4", 80), []byte{0x00, 0x50, 0x01, 1, 2, 3, 4}},
		{"domain", domainDestination(t, "example.com", 8080), append([]byte{0x1F, 0x90, 0x02, 11}, "example.com"...)},
		{"IPv6", ipDestination(t, "2001:db8::1", 443), []byte{0x01, 0xBB, 0x03, 0x20, 0x01, 0x0D, 0xB8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
	} {
		got, err := vmessaddr.AppendAddress(nil, tc.destination)
		if err != nil || !bytes.Equal(got, tc.want) {
			t.Errorf("%s: AppendAddress = % x, %v; want % x", tc.name, got, err, tc.want)
		}
	}
}

func TestAppendAddressKeepsWhatIsAlreadyInTheBuffer(t *testing.T) {
	got, err := vmessaddr.AppendAddress([]byte{0xAA}, ipDestination(t, "1.2.3.4", 80))
	if err != nil || !bytes.Equal(got, []byte{0xAA, 0x00, 0x50, 0x01, 1, 2, 3, 4}) {
		t.Errorf("AppendAddress = % x, %v", got, err)
	}
}

func TestAppendAddressRefusesWhatTheFormatCannotCarry(t *testing.T) {
	if _, err := vmessaddr.AppendAddress(nil, domainDestination(t, strings.Repeat("a", vmessaddr.MaxDomainLength+1), 1)); !errors.Is(err, vmessaddr.ErrDomainTooLong) {
		t.Errorf("long domain: error = %v, want ErrDomainTooLong", err)
	}
	if _, err := vmessaddr.AppendAddress(nil, domainDestination(t, strings.Repeat("a", vmessaddr.MaxDomainLength), 1)); err != nil {
		t.Errorf("domain of exactly 255 bytes: error = %v", err)
	}
	if _, err := vmessaddr.AppendAddress(nil, spec.Destination{}); err == nil {
		t.Error("a zero destination was accepted")
	}
}

func TestReadAddressAndParseAddressAgreeAndReportTheBytesTaken(t *testing.T) {
	for _, want := range []spec.Destination{
		ipDestination(t, "1.2.3.4", 53),
		ipDestination(t, "2001:db8::1", 443),
		domainDestination(t, "example.com", 8080),
		domainDestination(t, strings.Repeat("a", vmessaddr.MaxDomainLength), 65535),
		ipDestination(t, "0.0.0.0", 0),
	} {
		encoded, err := vmessaddr.AppendAddress(nil, want)
		if err != nil {
			t.Fatal(err)
		}
		withTrailer := append(bytes.Clone(encoded), "payload"...)

		parsed, length, err := vmessaddr.ParseAddress(withTrailer)
		if err != nil || parsed != want || length != len(encoded) {
			t.Errorf("ParseAddress(%s) = %s, %d, %v; want %s, %d", want, parsed, length, err, want, len(encoded))
		}
		reader := bytes.NewReader(withTrailer)
		read, err := vmessaddr.ReadAddress(reader)
		if err != nil || read != want {
			t.Errorf("ReadAddress(%s) = %s, %v", want, read, err)
		}
		if rest, _ := io.ReadAll(reader); string(rest) != "payload" {
			t.Errorf("ReadAddress(%s) left %q, want the trailer untouched", want, rest)
		}
	}
}

func TestMalformedInputIsRefusedByBothReaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		encoded []byte
		want    error
	}{
		{"empty", nil, io.ErrUnexpectedEOF},
		{"no type", []byte{0x00, 0x50}, io.ErrUnexpectedEOF},
		{"short IPv4", []byte{0x00, 0x50, vmessaddr.AddressIPv4, 1, 2, 3}, io.ErrUnexpectedEOF},
		{"short IPv6", []byte{0x00, 0x50, vmessaddr.AddressIPv6, 1, 2, 3}, io.ErrUnexpectedEOF},
		{"no domain length", []byte{0x00, 0x50, vmessaddr.AddressDomain}, io.ErrUnexpectedEOF},
		{"short domain", []byte{0x00, 0x50, vmessaddr.AddressDomain, 5, 'a', 'b'}, io.ErrUnexpectedEOF},
		{"empty domain", []byte{0x00, 0x50, vmessaddr.AddressDomain, 0}, vmessaddr.ErrEmptyDomain},
		{"type zero", []byte{0x00, 0x50, 0x00, 1, 2, 3, 4}, vmessaddr.ErrUnsupportedAddressType},
		{"type four", []byte{0x00, 0x50, 0x04, 1, 2, 3, 4}, vmessaddr.ErrUnsupportedAddressType},
	} {
		if _, _, err := vmessaddr.ParseAddress(tc.encoded); !errors.Is(err, tc.want) {
			t.Errorf("ParseAddress(%s): error = %v, want %v", tc.name, err, tc.want)
		}
		want := tc.want
		if len(tc.encoded) == 0 {
			want = io.EOF
		}
		if _, err := vmessaddr.ReadAddress(bytes.NewReader(tc.encoded)); !errors.Is(err, want) {
			t.Errorf("ReadAddress(%s): error = %v, want %v", tc.name, err, want)
		}
	}
}

// An address in the SOCKS5 layout (type first, IPv6 as 4) is not read as one of
// this package: it is refused or read as something else, never mistaken
func TestTheSocks5LayoutIsNotMistakenForThisOne(t *testing.T) {
	socks5IPv6 := append([]byte{0x04}, make([]byte, 16)...)
	socks5IPv6 = append(socks5IPv6, 0x01, 0xBB)
	if _, _, err := vmessaddr.ParseAddress(socks5IPv6); err == nil {
		t.Error("a SOCKS5 IPv6 address was read as a V2Ray one")
	}
}

func TestReadAddressUnmapsIPv4InIPv6(t *testing.T) {
	mapped := []byte{0x00, 0x50, vmessaddr.AddressIPv6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 1, 2, 3, 4}
	want := ipDestination(t, "1.2.3.4", 80)
	if got, _, err := vmessaddr.ParseAddress(mapped); err != nil || got != want {
		t.Errorf("ParseAddress = %s, %v; want %s", got, err, want)
	}
	if got, err := vmessaddr.ReadAddress(bytes.NewReader(mapped)); err != nil || got != want {
		t.Errorf("ReadAddress = %s, %v; want %s", got, err, want)
	}
}

func BenchmarkReadAddressIPv4(b *testing.B) {
	encoded, _ := vmessaddr.AppendAddress(nil, ipDestination(b, "1.2.3.4", 53))
	reader := bytes.NewReader(encoded)
	b.ReportAllocs()
	for b.Loop() {
		reader.Reset(encoded)
		if _, err := vmessaddr.ReadAddress(reader); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseAddressIPv4(b *testing.B) {
	encoded, _ := vmessaddr.AppendAddress(nil, ipDestination(b, "1.2.3.4", 53))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := vmessaddr.ParseAddress(encoded); err != nil {
			b.Fatal(err)
		}
	}
}
