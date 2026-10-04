package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/The-Apranik/Apranik/spec"
)

// ServerAddress is a host with an optional port: "8.8.8.8", "dns.google:853",
// "[2001:db8::1]:53". The port is left to the module, which knows its
// protocol's default; Destination applies it.
type ServerAddress struct {
	host string
	ip   netip.Addr
	port uint16
}

func (a ServerAddress) IsZero() bool { return a.host == "" && !a.ip.IsValid() }

func (a ServerAddress) String() string {
	host := a.host
	if a.ip.IsValid() {
		host = a.ip.String()
	}
	if a.port == 0 {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(int(a.port)))
}

// Destination is the address with defaultPort when it named none.
func (a ServerAddress) Destination(defaultPort uint16) (spec.Destination, error) {
	port := a.port
	if port == 0 {
		port = defaultPort
	}
	if a.ip.IsValid() {
		return spec.NewDestinationFromIP(a.ip, port)
	}
	return spec.NewDestinationFromDomain(a.host, port)
}

func (a ServerAddress) MarshalJSON() ([]byte, error) { return json.Marshal(a.String()) }

func (a *ServerAddress) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("a server address must be a quoted string such as \"8.8.8.8:53\", not %s", data)
	}
	parsed, err := ParseServerAddress(text)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// ParseServerAddress accepts an IP or a hostname, each with or without a port.
// A hostname is checked to be one, so no bytes but letters, digits, hyphens,
// underscores and dots reach a dial or a TLS server name.
func ParseServerAddress(text string) (ServerAddress, error) {
	if text == "" {
		return ServerAddress{}, errors.New("empty server address")
	}
	host, portText := text, ""
	if splitHost, splitPort, err := net.SplitHostPort(text); err == nil {
		host, portText = splitHost, splitPort
	} else if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		host = text[1 : len(text)-1]
	}

	var address ServerAddress
	if portText != "" {
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil || port == 0 {
			return ServerAddress{}, fmt.Errorf("server address %q: invalid port %q", text, portText)
		}
		address.port = uint16(port)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return ServerAddress{}, fmt.Errorf("server address %q: a zoned address is not supported", text)
		}
		address.ip = ip.Unmap()
		return address, nil
	}
	if !isHostname(host) {
		return ServerAddress{}, fmt.Errorf("server address %q: %q is neither an IP nor a hostname", text, host)
	}
	address.host = strings.ToLower(strings.TrimSuffix(host, "."))
	return address, nil
}

func isHostname(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for index := 0; index < len(label); index++ {
			character := label[index]
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || character == '-' || character == '_') {
				return false
			}
		}
	}
	return true
}
