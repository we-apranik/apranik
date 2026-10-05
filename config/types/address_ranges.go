package types

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

// AddressRanges is a list of IP addresses and ranges written as JSON strings:
// "10.0.0.2" (one address) or "104.16.0.0/13" (a range). A slice, so a value a
// module defaults is replaced, not merged, when the config file sets it.
type AddressRanges []netip.Prefix

// Contains reports whether the address is in any range. An IPv4-mapped IPv6
// address (::ffff:10.0.0.2) is compared as the IPv4 address it carries.
func (r AddressRanges) Contains(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range r {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// ParseAddressRanges is the one way an entry is read, so the config file and Go
// code agree.
func ParseAddressRanges(entries ...string) (AddressRanges, error) {
	ranges := make(AddressRanges, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		var prefix netip.Prefix
		if strings.Contains(entry, "/") {
			parsed, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("%q is not an address range such as 10.0.0.0/8: %w", entry, err)
			}
			prefix = parsed
		} else {
			address, err := netip.ParseAddr(entry)
			if err != nil {
				return nil, fmt.Errorf("%q is not an IP address or range: %w", entry, err)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		address := prefix.Addr()
		if address.Is4In6() {
			prefix = netip.PrefixFrom(address.Unmap(), prefix.Bits()-96)
		}
		ranges = append(ranges, prefix.Masked())
	}
	return ranges, nil
}

func (r *AddressRanges) UnmarshalJSON(data []byte) error {
	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("address ranges must be a list of strings: %w", err)
	}
	parsed, err := ParseAddressRanges(entries...)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func (r AddressRanges) MarshalJSON() ([]byte, error) {
	entries := make([]string, len(r))
	for index, prefix := range r {
		entries[index] = prefix.String()
	}
	return json.Marshal(entries)
}

// Covers everything, which makes a trust list meaningless.
func (r AddressRanges) CoversEverything() bool {
	for _, prefix := range r {
		if prefix.Bits() == 0 {
			return true
		}
	}
	return false
}
