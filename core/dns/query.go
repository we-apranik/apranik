package dns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

var ErrInvalidQuery = errors.New("dns: the query is not valid")

type QueryResult struct {
	ServerTag string

	Addresses []netip.Addr

	TTL time.Duration
}

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

func (r *Resolver) FlushCache() { r.caches.flush() }
