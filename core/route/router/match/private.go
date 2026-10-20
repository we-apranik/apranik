package match

import "net/netip"

// The ranges v2fly's geoip:private adds to what netip already classifies: addresses no public
// destination has, so traffic to them never goes to a proxy by mistake. 198.18.0.0/15 is also the
// fake-IP range of sing-box and Clash; a fake IP must be mapped back to its name before rules run.
var reservedIPv4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/3"),
}

// IsPrivateAddress is what the ip_is_private rule field reads: sing-box's set (private, loopback,
// link-local, multicast, unspecified) plus the extra reserved ranges in Xray's geoip:private.
func IsPrivateAddress(address netip.Addr) bool {
	address = address.Unmap()
	if address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() ||
		address.IsMulticast() || address.IsInterfaceLocalMulticast() || address.IsUnspecified() {
		return true
	}
	if !address.Is4() {
		return false
	}
	for _, prefix := range reservedIPv4Prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
