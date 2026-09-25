package config

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/The-Apranik/Apranik/spec"
)

// Host and port are separate fields in the config file, so a bare IPv6 literal
// needs no brackets -- which is most of why they are separate.

// A listen address must be a literal IP. A domain is refused rather than
// resolved: resolving is I/O, which the config package does not do, and one
// that answers with several addresses leaves "which one did it bind?"
// unanswerable.
func parseListenAddr(host string, port uint16) (spec.Endpoint, error) {
	if host == "" {
		return spec.Endpoint{}, fmt.Errorf("no address given")
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		if _, _, err := net.SplitHostPort(host); err == nil {
			return spec.Endpoint{}, fmt.Errorf("%q already contains a port; the port belongs in its own field", host)
		}
		return spec.Endpoint{}, fmt.Errorf("%q is not an IP address; a listen address cannot be a domain", host)
	}
	return spec.NewEndpoint(ip, port)
}

// A server address may be a domain, and stays unresolved: the transport
// resolves it when it dials.
func parseServerAddr(host string, port uint16) (spec.Destination, error) {
	if host == "" {
		return spec.Destination{}, fmt.Errorf("no address given")
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		return spec.NewDestinationFromIP(ip, port)
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return spec.Destination{}, fmt.Errorf("%q already contains a port; the port belongs in its own field", host)
	}
	return spec.NewDestinationFromDomain(host, port)
}

// The only two ways a location grows: into a field, or into an element. Every
// diagnostic location in this package is built from these, so no call site
// spells out a "." or a "[".
func fieldLocation(loc string, fields ...string) string {
	for _, field := range fields {
		if loc == "" {
			loc = field
			continue
		}
		loc += "." + field
	}
	return loc
}

func indexLocation(loc string, index int) string {
	return fmt.Sprintf("%s[%d]", loc, index)
}
