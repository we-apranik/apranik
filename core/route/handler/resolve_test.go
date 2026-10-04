package handler

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/spec"
)

type countingDomainResolver struct {
	answer     []netip.Addr
	err        error
	calls      int
	lastServer string
}

func (r *countingDomainResolver) ResolveDomain(context.Context, string) ([]netip.Addr, error) {
	r.calls++
	r.lastServer = ""
	return r.answer, r.err
}

func (r *countingDomainResolver) ResolveDomainWithServer(_ context.Context, _ string, serverTag string) ([]netip.Addr, error) {
	r.calls++
	r.lastServer = serverTag
	return r.answer, r.err
}

func domainMetadata(t *testing.T, domain string) spec.Metadata {
	t.Helper()
	destination, err := spec.NewDestinationFromDomain(domain, 443)
	if err != nil {
		t.Fatal(err)
	}
	return spec.Metadata{Network: spec.NetworkTCP, Destination: destination}
}

func TestInformantsResolveOnceAndRemember(t *testing.T) {
	resolver := &countingDomainResolver{answer: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}
	informants := map[string]interface {
		Resolve(context.Context, string) []netip.Addr
	}{
		"stream": newStreamTrafficInformant(domainMetadata(t, "example.com"), nil, resolver, logging.Discard()),
		"udp":    &udpSessionTrafficInformant{metadata: domainMetadata(t, "example.com"), resolver: resolver, logger: logging.Discard()},
	}
	for name, informant := range informants {
		resolver.calls = 0
		first := informant.Resolve(context.Background(), "cf")
		second := informant.Resolve(context.Background(), "")
		if !slices.Equal(first, resolver.answer) || !slices.Equal(second, resolver.answer) {
			t.Errorf("%s: got %v and %v", name, first, second)
		}
		if resolver.calls != 1 || resolver.lastServer != "cf" {
			t.Errorf("%s: %d lookups, last with %q; want one with cf", name, resolver.calls, resolver.lastServer)
		}
	}
}

func TestInformantResolvesNothingForAnIPOrAFailure(t *testing.T) {
	resolver := &countingDomainResolver{answer: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}
	destination, _ := spec.NewDestinationFromIP(netip.MustParseAddr("8.8.8.8"), 443)
	byIP := newStreamTrafficInformant(spec.Metadata{Destination: destination}, nil, resolver, logging.Discard())
	if got := byIP.Resolve(context.Background(), ""); got != nil || resolver.calls != 0 {
		t.Errorf("an IP destination: got %v after %d lookups", got, resolver.calls)
	}

	failing := &countingDomainResolver{answer: []netip.Addr{netip.MustParseAddr("192.0.2.1")}, err: errors.New("timeout")}
	informant := newStreamTrafficInformant(domainMetadata(t, "example.com"), nil, failing, logging.Discard())
	if got := informant.Resolve(context.Background(), ""); got != nil {
		t.Errorf("a failed lookup gave %v", got)
	}

	withoutResolver := newStreamTrafficInformant(domainMetadata(t, "example.com"), nil, nil, logging.Discard())
	if got := withoutResolver.Resolve(context.Background(), ""); got != nil {
		t.Errorf("no resolver gave %v", got)
	}
}
