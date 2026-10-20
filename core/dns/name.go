package dns

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	maxDomainNameLength = 253 // without the trailing dot
	maxLabelLength      = 63
)

var errEmptyDomain = errors.New("dns: empty domain")

type lookupName struct {
	text string
}

func (n lookupName) qualifiedName() (dnsmessage.Name, error) {
	return dnsmessage.NewName(n.text + ".")
}

// parseLookupName refuses anything that is not a plain hostname -- spaces,
// control bytes, non-ASCII (an IDN must arrive in its ASCII form), empty
// labels -- so a query never carries bytes a client smuggled into a domain.
// Underscores are allowed: service names use them.
func parseLookupName(domain string) (lookupName, error) {
	text := strings.ToLower(strings.TrimSuffix(domain, "."))
	if text == "" {
		return lookupName{}, errEmptyDomain
	}
	if len(text) > maxDomainNameLength {
		return lookupName{}, fmt.Errorf("dns: domain longer than %d bytes", maxDomainNameLength)
	}
	labelLength := 0
	for index := 0; index <= len(text); index++ {
		if index == len(text) || text[index] == '.' {
			if labelLength == 0 {
				return lookupName{}, fmt.Errorf("dns: domain %q has an empty label", text)
			}
			labelLength = 0
			continue
		}
		if !isHostnameByte(text[index]) {
			return lookupName{}, fmt.Errorf("dns: domain %q has a byte a hostname cannot carry", text)
		}
		if labelLength++; labelLength > maxLabelLength {
			return lookupName{}, fmt.Errorf("dns: domain %q has a label longer than %d bytes", text, maxLabelLength)
		}
	}
	return lookupName{text: text}, nil
}

func isDigit(character byte) bool { return character >= '0' && character <= '9' }

func isHostnameByte(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
		character == '-' || character == '_'
}

func literalAddress(domain string) (netip.Addr, bool) {
	domain = strings.TrimSuffix(strings.TrimPrefix(domain, "["), "]")
	if domain == "" || !strings.Contains(domain, ":") && !isDigit(domain[len(domain)-1]) {
		return netip.Addr{}, false
	}
	address, err := netip.ParseAddr(domain)
	if err != nil || address.Zone() != "" {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

// isLocalhost is RFC 6761's localhost and every name under it: always the
// loopback address, never sent to a server, where it would leak or be
// answered with something else.
func isLocalhost(name string) bool {
	return name == "localhost" || strings.HasSuffix(name, ".localhost")
}
