package apicoreprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/spec"
)

var errNoResolver = errors.New("apicoreprovider: no resolver was given")

// QueryDNS is one lookup through the resolver, by its rules and cache.
func (p *Provider) QueryDNS(ctx context.Context, name, recordType string) (spec.APIDNSAnswer, error) {
	if p.resolver == nil {
		return spec.APIDNSAnswer{}, errNoResolver
	}
	var wanted dnsmessage.Type
	switch strings.ToUpper(recordType) {
	case "A":
		wanted = dnsmessage.TypeA
	case "AAAA":
		wanted = dnsmessage.TypeAAAA
	default:
		return spec.APIDNSAnswer{}, fmt.Errorf("%w: record type %q: only A and AAAA are looked up", spec.ErrAPIInvalidDNSQuery, recordType)
	}

	result, err := p.resolver.Query(ctx, name, wanted)
	if errors.Is(err, dns.ErrInvalidQuery) {
		return spec.APIDNSAnswer{}, fmt.Errorf("%w: %w", spec.ErrAPIInvalidDNSQuery, err)
	}
	if err != nil {
		return spec.APIDNSAnswer{}, err
	}

	answer := spec.APIDNSAnswer{ServerTag: result.ServerTag, Records: make([]spec.APIDNSRecord, len(result.Addresses))}
	owner := strings.ToLower(strings.TrimSuffix(name, ".")) + "."
	for index, address := range result.Addresses {
		answer.Records[index] = spec.APIDNSRecord{
			Name: owner,
			Type: strings.ToUpper(recordType),
			TTL:  result.TTL,
			Data: address.String(),
		}
	}
	return answer, nil
}

func (p *Provider) FlushDNSCache() error {
	if p.resolver == nil {
		return errNoResolver
	}
	p.logger.Info("DNS cache flush requested")
	p.resolver.FlushCache()
	return nil
}
