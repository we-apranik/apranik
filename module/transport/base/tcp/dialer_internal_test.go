package tcp

import (
	"net/netip"
	"slices"
	"testing"
)

func TestInterleaveAddressFamiliesStartsWithTheResolversFirst(t *testing.T) {
	parse := func(texts ...string) []netip.Addr {
		var parsed []netip.Addr
		for _, text := range texts {
			parsed = append(parsed, netip.MustParseAddr(text))
		}
		return parsed
	}
	for _, tc := range []struct{ in, want []netip.Addr }{
		{parse("192.0.2.1", "192.0.2.2", "2001:db8::1", "2001:db8::2"), parse("192.0.2.1", "2001:db8::1", "192.0.2.2", "2001:db8::2")},
		{parse("2001:db8::1", "192.0.2.1", "192.0.2.2"), parse("2001:db8::1", "192.0.2.1", "192.0.2.2")},
		{parse("192.0.2.1", "192.0.2.2"), parse("192.0.2.1", "192.0.2.2")},
		{parse("192.0.2.1"), parse("192.0.2.1")},
	} {
		if got := interleaveAddressFamilies(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("%v: got %v, want %v", tc.in, got, tc.want)
		}
	}
}
