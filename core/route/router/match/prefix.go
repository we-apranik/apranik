package match

import (
	"cmp"
	"encoding/binary"
	"net/netip"
	"slices"
)

// The ranges are merged and sorted when built, so a lookup is one binary search whatever the number of
// prefixes.
type PrefixMatcher struct {
	ipv4 []ipv4Range
	ipv6 []ipv6Range
}

type ipv4Range struct{ first, last uint32 }

type ipv6Range struct{ first, last netip.Addr }

func NewPrefixMatcher(prefixes []netip.Prefix) *PrefixMatcher {
	var ipv4 []ipv4Range
	var ipv6 []ipv6Range
	for _, prefix := range prefixes {
		if !prefix.IsValid() {
			continue
		}
		prefix = unmapPrefix(prefix.Masked())
		if prefix.Addr().Is4() {
			first := ipv4AsNumber(prefix.Addr())
			hostBits := 32 - prefix.Bits()
			last := first | uint32(uint64(1)<<hostBits-1)
			ipv4 = append(ipv4, ipv4Range{first, last})
			continue
		}
		ipv6 = append(ipv6, ipv6Range{prefix.Addr(), lastAddressOf(prefix)})
	}
	return &PrefixMatcher{ipv4: mergeIPv4Ranges(ipv4), ipv6: mergeIPv6Ranges(ipv6)}
}

func (m *PrefixMatcher) Contains(address netip.Addr) bool {
	address = address.Unmap()
	if address.Is4() {
		number := ipv4AsNumber(address)
		index, _ := slices.BinarySearchFunc(m.ipv4, number, func(r ipv4Range, target uint32) int {
			return cmp.Compare(r.first, target)
		})
		if index < len(m.ipv4) && m.ipv4[index].first == number {
			return true
		}
		return index > 0 && m.ipv4[index-1].last >= number
	}
	if !address.Is6() {
		return false
	}
	address = address.WithZone("")
	index, _ := slices.BinarySearchFunc(m.ipv6, address, func(r ipv6Range, target netip.Addr) int {
		return r.first.Compare(target)
	})
	if index < len(m.ipv6) && m.ipv6[index].first == address {
		return true
	}
	return index > 0 && m.ipv6[index-1].last.Compare(address) >= 0
}

func (m *PrefixMatcher) Len() int { return len(m.ipv4) + len(m.ipv6) }

func unmapPrefix(prefix netip.Prefix) netip.Prefix {
	if !prefix.Addr().Is4In6() {
		return prefix
	}
	bits := prefix.Bits() - 96
	if bits < 0 {
		bits = 0
	}
	return netip.PrefixFrom(prefix.Addr().Unmap(), bits).Masked()
}

func ipv4AsNumber(address netip.Addr) uint32 {
	bytes := address.As4()
	return binary.BigEndian.Uint32(bytes[:])
}

func lastAddressOf(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Addr().As16()
	for bit := prefix.Bits(); bit < 128; bit++ {
		bytes[bit/8] |= 1 << (7 - bit%8)
	}
	return netip.AddrFrom16(bytes)
}

func mergeIPv4Ranges(ranges []ipv4Range) []ipv4Range {
	slices.SortFunc(ranges, func(a, b ipv4Range) int { return cmp.Compare(a.first, b.first) })
	merged := ranges[:0]
	for _, next := range ranges {
		if len(merged) > 0 {
			previous := &merged[len(merged)-1]
			if previous.last == ^uint32(0) || next.first <= previous.last+1 {
				previous.last = max(previous.last, next.last)
				continue
			}
		}
		merged = append(merged, next)
	}
	return slices.Clip(merged)
}

func mergeIPv6Ranges(ranges []ipv6Range) []ipv6Range {
	slices.SortFunc(ranges, func(a, b ipv6Range) int { return a.first.Compare(b.first) })
	merged := ranges[:0]
	for _, next := range ranges {
		if len(merged) > 0 {
			previous := &merged[len(merged)-1]
			following := previous.last.Next()
			if !following.IsValid() || next.first.Compare(following) <= 0 {
				if next.last.Compare(previous.last) > 0 {
					previous.last = next.last
				}
				continue
			}
		}
		merged = append(merged, next)
	}
	return slices.Clip(merged)
}
