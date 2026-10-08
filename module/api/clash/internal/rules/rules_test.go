package rules_test

import (
	"net/http"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/rules"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, _ *stream.Hub) { rules.Register(router, provider) })
}

func TestRulesAreApranikRulesInClashsEnvelope(t *testing.T) {
	provider := &testkit.Provider{RoutingRulesFunc: func() ([]spec.APIRoutingRule, error) {
		return []spec.APIRoutingRule{
			{Location: "routing.rules[0]", Action: spec.APIRoutingRuleActionSniff, Match: "*", Hits: 0, HitsCounted: true},
			{Location: "routing.rules[1]", Action: spec.APIRoutingRuleActionRoute, Match: "domain_suffix=ir port=443", OutboundTag: "direct", Hits: 12, HitsCounted: true},
			{Location: "routing.rules[2]", Action: spec.APIRoutingRuleActionBlock, Match: "network=udp"},
			{Location: "routing.rules[3]", Action: spec.APIRoutingRuleActionResolve, Match: "ip=10.0.0.0/8", DNSServerTag: "local"},
			{Location: "default", Action: spec.APIRoutingRuleActionRoute, OutboundTag: "proxy", Hits: 5, HitsCounted: true},
		}, nil
	}}
	var reply struct {
		Rules []struct {
			Index    int     `json:"index"`
			Type     string  `json:"type"`
			Payload  string  `json:"payload"`
			Proxy    string  `json:"proxy"`
			HitCount *uint64 `json:"hitCount"`
		} `json:"rules"`
	}
	if status := mount(t, provider).GetJSON("/rules", &reply); status != http.StatusOK || len(reply.Rules) != 5 {
		t.Fatalf("status %d, %d rules", status, len(reply.Rules))
	}
	r := reply.Rules
	if r[0].Type != "sniff" || r[0].Payload != "*" || r[0].Proxy != "" || r[0].HitCount == nil || *r[0].HitCount != 0 {
		t.Errorf("rule 0 = %+v", r[0])
	}
	if r[1].Index != 1 || r[1].Type != "route" || r[1].Payload != "domain_suffix=ir port=443" || r[1].Proxy != "direct" || *r[1].HitCount != 12 {
		t.Errorf("rule 1 = %+v", r[1])
	}
	if r[2].Type != "block" || r[2].Proxy != "" || r[2].HitCount != nil {
		t.Errorf("rule 2 = %+v (no hit count when statistics are off)", r[2])
	}
	if r[3].Type != "resolve" || r[3].Proxy != "local" {
		t.Errorf("rule 3 = %+v", r[3])
	}
	if r[4].Type != "default" || r[4].Proxy != "proxy" || r[4].Index != 4 || *r[4].HitCount != 5 {
		t.Errorf("default = %+v", r[4])
	}
}
