package apicoreprovider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/we-apranik/apranik/core/apicoreprovider"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/dns"
	"github.com/we-apranik/apranik/spec"
)

func providerWithResolver(t *testing.T) *apicoreprovider.Provider {
	t.Helper()
	resolver := dns.NewSystemResolver(nil)
	t.Cleanup(func() { resolver.Close() })
	provider, err := apicoreprovider.New(apicoreprovider.RunningCore{Blueprint: &blueprint.Blueprint{}, Resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestQueryDNSReturnsEachAddressAsARecordOfTheTypeAsked(t *testing.T) {
	provider := providerWithResolver(t)

	answer, err := provider.QueryDNS(context.Background(), "LocalHost", "aaaa")
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Records) != 1 || answer.Records[0] != (spec.APIDNSRecord{Name: "localhost.", Type: "AAAA", Data: "::1"}) {
		t.Errorf("records = %+v, want one AAAA record of localhost.", answer.Records)
	}

	answer, err = provider.QueryDNS(context.Background(), "localhost", "A")
	if err != nil || len(answer.Records) != 1 || answer.Records[0].Data != "127.0.0.1" || answer.Records[0].Type != "A" {
		t.Errorf("A answer = %+v, %v", answer, err)
	}
}

func TestQueryDNSRefusesATypeOrANameItCannotLookUp(t *testing.T) {
	provider := providerWithResolver(t)
	for _, call := range []struct{ name, recordType string }{
		{"example.com", "MX"},
		{"example.com", ""},
		{"", "A"},
		{"192.0.2.1", "A"},
		{"has space.example", "A"},
	} {
		if _, err := provider.QueryDNS(context.Background(), call.name, call.recordType); !errors.Is(err, spec.ErrAPIInvalidDNSQuery) {
			t.Errorf("QueryDNS(%q, %q) = %v, want ErrAPIInvalidDNSQuery", call.name, call.recordType, err)
		}
	}
}

func TestQueryDNSAndFlushNeedAResolver(t *testing.T) {
	provider, err := apicoreprovider.New(apicoreprovider.RunningCore{Blueprint: &blueprint.Blueprint{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.QueryDNS(context.Background(), "localhost", "A"); err == nil {
		t.Error("QueryDNS without a resolver succeeded")
	}
	if err := provider.FlushDNSCache(); err == nil {
		t.Error("FlushDNSCache without a resolver succeeded")
	}
}

func TestFlushDNSCacheSucceedsWhenThereIsNothingToForget(t *testing.T) {
	if err := providerWithResolver(t).FlushDNSCache(); err != nil {
		t.Errorf("FlushDNSCache = %v", err)
	}
}
