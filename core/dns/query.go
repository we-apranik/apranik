package dns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// ErrInvalidQuery: the name is not a domain name, or the record type is not one
// the resolver looks up. Only A and AAAA are.
var ErrInvalidQuery = errors.New("dns: the query is not valid")

// QueryResult is what one Query found.
type QueryResult struct {
	// The DNS server asked, or the one whose cached answer was used; empty for
	// "localhost" names, which no server is asked about.
	ServerTag string

	// Empty when the name exists without records of this type.
	Addresses []netip.Addr

	// How long the answer may be reused, as the server gave it. Zero for an
	// answer that is not cacheable.
	TTL time.Duration
}

// Query is one lookup of one record type, by the DNS rules and the cache, for
// whoever wants to see what the resolver answers (an API's DNS query). Unlike
// ResolveDomain it asks for exactly the record type given, not the ip_versions,
// and keeps the TTL.
func (r *Resolver) Query(ctx context.Context, domain string, recordType dnsmessage.Type) (QueryResult, error) {
	if recordType != dnsmessage.TypeA && recordType != dnsmessage.TypeAAAA {
		return QueryResult{}, fmt.Errorf("%w: only A and AAAA records are looked up", ErrInvalidQuery)
	}
	if _, isLiteral := literalAddress(domain); isLiteral {
		return QueryResult{}, fmt.Errorf("%w: %q is an address, not a domain name", ErrInvalidQuery, domain)
	}
	name, err := parseLookupName(domain)
	if err != nil {
		return QueryResult{}, fmt.Errorf("%w: %w", ErrInvalidQuery, err)
	}
	if isLocalhost(name.text) {
		return QueryResult{Addresses: loopbackAddressesOfType(recordType)}, nil
	}

	servers := r.servers.Load()
	if servers == nil {
		return QueryResult{}, errResolverNotReady
	}
	chosen := servers.choose(name.text)

	r.stats.LookupStarted()
	found, err := r.lookupRecordType(ctx, name, chosen, false, recordType)
	if err != nil {
		r.stats.LookupFailed(lookupFailureReason(err))
		return QueryResult{}, lookupError{cause: err}
	}
	return QueryResult{ServerTag: chosen.tag, Addresses: found.addresses, TTL: found.ttl}, nil
}

func loopbackAddressesOfType(recordType dnsmessage.Type) []netip.Addr {
	if recordType == dnsmessage.TypeA {
		return []netip.Addr{netip.AddrFrom4([4]byte{127, 0, 0, 1})}
	}
	return []netip.Addr{netip.IPv6Loopback()}
}

// FlushCache forgets every cached answer, stale ones included. A lookup in
// flight still stores its answer when it ends.
func (r *Resolver) FlushCache() { r.caches.flush() }
