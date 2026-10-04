package handler

import (
	"context"
	"net/netip"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/spec"
)

// DomainResolver is what the routing resolve action asks: core/dns's
// Resolver.
type DomainResolver interface {
	ResolveDomain(ctx context.Context, domain string) ([]netip.Addr, error)
	ResolveDomainWithServer(ctx context.Context, domain, serverTag string) ([]netip.Addr, error)
}

// destinationResolution is one informant's resolve: done once, remembered.
type destinationResolution struct {
	done      bool
	addresses []netip.Addr
}

func (r *destinationResolution) resolve(ctx context.Context, resolver DomainResolver, logger *logging.Logger, destination spec.Destination, serverTag string) []netip.Addr {
	if r.done {
		return r.addresses
	}
	r.done = true

	domain, isDomain := destination.Domain()
	if !isDomain || resolver == nil {
		return nil
	}
	var err error
	if serverTag == "" {
		r.addresses, err = resolver.ResolveDomain(ctx, domain)
	} else {
		r.addresses, err = resolver.ResolveDomainWithServer(ctx, domain, serverTag)
	}
	if err != nil {
		// The rules after the resolve see no addresses; the request goes on.
		logger.Debug("Resolve for routing failed", "domain", domain, "error", err)
		r.addresses = nil
	}
	return r.addresses
}
