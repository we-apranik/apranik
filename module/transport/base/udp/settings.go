package udp

import (
	"encoding/json"
	"errors"
)

// Addresses are parameters, not settings: a listener is handed its bind
// address, and a dialer whichever local address the caller wants. Resolving is
// the resolver's, so its timeout and cache are configured in the
// dns section, not here.
type Settings struct {
	// Moved to the dns section; read only to say so.
	MovedResolveTimeout  json.RawMessage `json:"resolve_timeout,omitempty"`
	MovedResolveCacheTTL json.RawMessage `json:"resolve_cache_ttl,omitempty"`
}

func (s *Settings) Validate() error {
	var errs []error
	if s.MovedResolveTimeout != nil {
		errs = append(errs, errors.New(`udp: resolve_timeout moved to the dns section: each DNS server bounds its own queries with "settings": {"timeout": "5s"}`))
	}
	if s.MovedResolveCacheTTL != nil {
		errs = append(errs, errors.New(`udp: resolve_cache_ttl moved to the dns section: answers are cached for their TTL, configured under "dns": {"cache": {...}}`))
	}
	return errors.Join(errs...)
}

func (s *Settings) Warnings() []string { return nil }
