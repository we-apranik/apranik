package config

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/we-apranik/apranik/spec"
)

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

// The only two ways a location grows
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
