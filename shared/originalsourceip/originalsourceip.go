// Package originalsourceip decides which client address a request's forwarding
// header names, and whether to believe it. A proxy in front of the server
// appends the address it saw to a header such as X-Forwarded-For; anyone can
// also write that header, so the address is taken from the right, past the
package originalsourceip

import (
	"crypto/subtle"
	"net/http"
	"net/netip"
	"strings"

	"github.com/we-apranik/apranik/spec"
)

type Config struct {
	Headers []string

	TrustedProxies []netip.Prefix

	ProofHeaderName  string
	ProofHeaderValue string
}

type Resolver struct{ config Config }

func New(config Config) *Resolver { return &Resolver{config: config} }

func (r *Resolver) Resolve(header http.Header, below spec.Endpoint) (spec.Endpoint, bool) {
	if len(r.config.TrustedProxies) > 0 && !r.trusted(below.Addr()) {
		return spec.Endpoint{}, false
	}
	if !r.proven(header) {
		return spec.Endpoint{}, false
	}
	entries, found := r.entries(header)
	if !found {
		return spec.Endpoint{}, false
	}

	for index := len(entries) - 1; index >= 0; index-- {
		endpoint, parsed := parseEntry(entries[index])
		if !parsed {
			return spec.Endpoint{}, false
		}
		if len(r.config.TrustedProxies) > 0 && r.trusted(endpoint.Addr()) {
			if index == 0 {
				return endpoint, true
			}
			continue
		}
		return endpoint, true
	}
	return spec.Endpoint{}, false
}

func (r *Resolver) trusted(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range r.config.TrustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (r *Resolver) proven(header http.Header) bool {
	if r.config.ProofHeaderName == "" {
		return true
	}
	values := header.Values(r.config.ProofHeaderName)
	if len(values) == 0 {
		return false
	}
	if r.config.ProofHeaderValue == "" {
		return true
	}
	matched := 0
	for _, value := range values {
		matched |= subtle.ConstantTimeCompare([]byte(strings.TrimSpace(value)), []byte(r.config.ProofHeaderValue))
	}
	return matched == 1
}

func (r *Resolver) entries(header http.Header) ([]string, bool) {
	for _, name := range r.config.Headers {
		lines := header.Values(name)
		if len(lines) == 0 {
			continue
		}
		var entries []string
		for _, line := range lines {
			for _, entry := range strings.Split(line, ",") {
				entries = append(entries, strings.TrimSpace(entry))
			}
		}
		return entries, len(entries) > 0
	}
	return nil, false
}

func parseEntry(entry string) (spec.Endpoint, bool) {
	var address netip.Addr
	var port uint16
	if withPort, err := netip.ParseAddrPort(entry); err == nil {
		address, port = withPort.Addr(), withPort.Port()
	} else {
		bare := strings.TrimSuffix(strings.TrimPrefix(entry, "["), "]")
		parsed, err := netip.ParseAddr(bare)
		if err != nil {
			return spec.Endpoint{}, false
		}
		address = parsed
	}
	if address.Zone() != "" {
		return spec.Endpoint{}, false
	}
	endpoint, err := spec.NewEndpoint(address.Unmap(), port)
	if err != nil {
		return spec.Endpoint{}, false
	}
	return endpoint, true
}
