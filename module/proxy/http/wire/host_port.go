package wire

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/The-Apranik/Apranik/spec"
)

// ParseHostPort keeps a domain unresolved: only the base transport resolves.
// A defaultPort of 0 means the port is required.
func ParseHostPort(hostPort string, defaultPort uint16) (spec.Destination, error) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		if defaultPort == 0 {
			return spec.Destination{}, fmt.Errorf("http: %q has no port: %w", hostPort, err)
		}
		host, port = strings.TrimSuffix(strings.TrimPrefix(hostPort, "["), "]"), strconv.Itoa(int(defaultPort))
	}
	if host == "" {
		return spec.Destination{}, fmt.Errorf("http: %q has no host", hostPort)
	}

	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return spec.Destination{}, fmt.Errorf("http: %q has no usable port", hostPort)
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		return spec.NewDestinationFromIP(ip, uint16(portNumber))
	}
	return spec.NewDestinationFromDomain(host, uint16(portNumber))
}

func FormatHostPort(destination spec.Destination) (string, error) {
	switch {
	case !destination.IsValid():
		return "", errors.New("http: address has neither domain nor IP")
	case destination.Port() == 0:
		return "", errors.New("http: address has no port")
	default:
		return destination.String(), nil
	}
}
