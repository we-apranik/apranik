package socksaddr_test

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

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
