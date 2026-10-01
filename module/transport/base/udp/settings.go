package udp

import (
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
)

// Addresses are parameters, not settings: a listener is handed its bind
// address, and a dialer whichever local address the caller wants.
type Settings struct {
	// Bounds one lookup of a domain a write is addressed to. WriteTo has no
	// context of its own, so without it a resolver that never answers stalls
	// the write path.
	ResolveTimeout types.Duration `json:"resolve_timeout"`

	// How long a resolved domain is reused. Go's resolver does not report the
	// record's TTL, so this is a fixed compromise.
	ResolveCacheTTL types.Duration `json:"resolve_cache_ttl"`
}

func (s *Settings) Validate() error {
	if time.Duration(s.ResolveTimeout) < 0 {
		return fmt.Errorf("udp: resolve_timeout %s must not be negative", s.ResolveTimeout)
	}
	if time.Duration(s.ResolveCacheTTL) < 0 {
		return fmt.Errorf("udp: resolve_cache_ttl %s must not be negative", s.ResolveCacheTTL)
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }

// Zero means the default, so a Transport built without its constructor works.
func (s *Settings) resolverPolicy() resolverPolicy {
	policy := resolverPolicy{timeout: time.Duration(s.ResolveTimeout), cacheTTL: time.Duration(s.ResolveCacheTTL)}
	if policy.timeout == 0 {
		policy.timeout = DefaultResolveTimeout
	}
	if policy.cacheTTL == 0 {
		policy.cacheTTL = DefaultResolveCacheTTL
	}
	return policy
}
