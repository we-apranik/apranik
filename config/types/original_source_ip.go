package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/we-apranik/apranik/shared/originalsourceip"
	"github.com/we-apranik/apranik/spec"
)

// OriginalSourceIP is the settings block `ws` and `httpupgrade` share for
// reading the client's IP from a header a proxy in front added. A module starts
// from DefaultOriginalSourceIP before the config file is decoded; Headers is a
// slice, so a config file replaces the default instead of merging into it.
type OriginalSourceIP struct {
	// Turns the feature off: the header is never read and the source stays the
	// address the layer below reported.
	Disabled bool `json:"disabled"`

	// Header names, in order; the first one the request carries is read.
	Headers []string `json:"headers"`

	// Without it the header is believed from any peer.
	Verification OriginalSourceIPVerification `json:"verification"`
}

type OriginalSourceIPVerification struct {
	// The peer must be one of these, and they are skipped when the header is
	// read from the right. Empty means the peer is not checked.
	TrustedProxyAddresses AddressRanges `json:"trusted_proxy_addresses"`

	// A header the proxy adds. Nil means no proof is required.
	ProofHeader *ProofHeader `json:"proof_header"`
}

type ProofHeader struct {
	Name string `json:"name"`

	// Empty means the header only has to be present: a leaked header name is
	// then enough to pass, which a value is not.
	Value Secret `json:"value"`
}

func DefaultOriginalSourceIP() OriginalSourceIP {
	return OriginalSourceIP{Headers: []string{"X-Forwarded-For"}}
}

// Unverified reports whether the header would be believed from any peer.
func (o *OriginalSourceIP) Unverified() bool {
	return !o.Disabled && len(o.Verification.TrustedProxyAddresses) == 0 && o.Verification.ProofHeader == nil
}

func (o *OriginalSourceIP) Validate() error {
	if len(o.Headers) == 0 {
		return errors.New("headers must name at least one header; set disabled to true to stop reading one")
	}
	for _, name := range o.Headers {
		if !isHeaderName(name) {
			return fmt.Errorf("headers: %q is not an HTTP header name", name)
		}
	}
	if proof := o.Verification.ProofHeader; proof != nil {
		if !isHeaderName(proof.Name) {
			return fmt.Errorf("verification.proof_header.name %q is not an HTTP header name", proof.Name)
		}
		for _, name := range o.Headers {
			if strings.EqualFold(name, proof.Name) {
				return fmt.Errorf("verification.proof_header.name %q is also in headers; the proof must be a different header", proof.Name)
			}
		}
	}
	return nil
}

func (o *OriginalSourceIP) Warnings() []string {
	var warnings []string
	if o.Verification.TrustedProxyAddresses.CoversEverything() {
		warnings = append(warnings, "verification.trusted_proxy_addresses covers every address, so it checks nothing")
	}
	if o.Disabled && (len(o.Verification.TrustedProxyAddresses) > 0 || o.Verification.ProofHeader != nil) {
		warnings = append(warnings, "original_source_ip is disabled, so its verification is unused")
	}
	return warnings
}

// WarningsForListenAddress is the part of the module's warnings that depends on
// where the inbound listens: an unverified header is safe only when no one but
// a proxy on this machine can reach the port. module names the caller.
func (o *OriginalSourceIP) WarningsForListenAddress(module string, listen spec.Endpoint) []string {
	if !o.Unverified() || listen.Addr().IsLoopback() {
		return nil
	}
	return []string{fmt.Sprintf("%s: original_source_ip reads the client's IP from %s with no verification, so any client that can reach this inbound can choose its own source IP; set verification, or set disabled to true if no proxy is in front",
		module, strings.Join(o.Headers, ", "))}
}

// NewResolver is nil when the feature is disabled.
func (o *OriginalSourceIP) NewResolver() *originalsourceip.Resolver {
	if o.Disabled {
		return nil
	}
	config := originalsourceip.Config{Headers: o.Headers, TrustedProxies: o.Verification.TrustedProxyAddresses}
	if proof := o.Verification.ProofHeader; proof != nil {
		config.ProofHeaderName = proof.Name
		config.ProofHeaderValue = proof.Value.Reveal()
	}
	return originalsourceip.New(config)
}

// RFC 9110 token characters.
func isHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", character):
		default:
			return false
		}
	}
	return true
}
