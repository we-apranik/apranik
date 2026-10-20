package types

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
)

type AddressRanges []netip.Prefix

func (r AddressRanges) Contains(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range r {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

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

func (r AddressRanges) CoversEverything() bool {
	for _, prefix := range r {
		if prefix.Bits() == 0 {
			return true
		}
	}
	return false
}
