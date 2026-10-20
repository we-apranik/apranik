package informant

import (
	"context"
	"net/netip"

	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

type DomainResolver interface {
	ResolveDomain(ctx context.Context, domain string) ([]netip.Addr, error)
	ResolveDomainWithServer(ctx context.Context, domain, serverTag string) ([]netip.Addr, error)
}

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
		logger.Debug("Resolve for routing failed", "domain", domain, "error", err)
		r.addresses = nil
	}
	return r.addresses
}
