package apicoreprovider_test

import (
	"net/netip"
	"regexp"
	"testing"

	"github.com/The-Apranik/Apranik/core/apicoreprovider"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

func providerWithRouting(t *testing.T, routing *blueprint.Routing, registry *stats.Registry) *apicoreprovider.Provider {
	t.Helper()
	provider, err := apicoreprovider.New(apicoreprovider.Parts{Blueprint: &blueprint.Blueprint{Routing: routing}, Stats: registry})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func route(outbound string) blueprint.Action {
	return blueprint.Action{Route: &blueprint.RouteAction{Outbound: outbound}}
}

func TestRoutingRulesListEachRuleInOrderThenTheDefault(t *testing.T) {
	provider := providerWithRouting(t, &blueprint.Routing{
		Rules: []blueprint.Rule{
			{Action: blueprint.Action{Sniff: &blueprint.SniffAction{}}},
			{Match: blueprint.Match{DomainSuffixes: []string{"a.com", "b.com"}, Ports: []blueprint.PortRange{{First: 443, Last: 443}}}, Action: route("proxy")},
			{Match: blueprint.Match{Network: spec.NetworkUDP}, Action: blueprint.Action{Block: true}},
			{Match: blueprint.Match{IPs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}, Action: blueprint.Action{Resolve: &blueprint.ResolveAction{Server: "local"}}},
		},
		DefaultAction: route("direct"),
	}, nil)

	rules, err := provider.RoutingRules()
	if err != nil {
		t.Fatal(err)
	}
	want := []spec.APIRoutingRule{
		{Location: "routing.rules[0]", Action: spec.APIRoutingRuleActionSniff, Match: "*"},
		{Location: "routing.rules[1]", Action: spec.APIRoutingRuleActionRoute, Match: "domain_suffix=a.com,b.com port=443", OutboundTag: "proxy"},
		{Location: "routing.rules[2]", Action: spec.APIRoutingRuleActionBlock, Match: "network=udp"},
		{Location: "routing.rules[3]", Action: spec.APIRoutingRuleActionResolve, Match: "ip=10.0.0.0/8", DNSServerTag: "local"},
		{Location: "default", Action: spec.APIRoutingRuleActionRoute, OutboundTag: "direct"},
	}
	if len(rules) != len(want) {
		t.Fatalf("%d rules, want %d: %+v", len(rules), len(want), rules)
	}
	for index := range want {
		if rules[index] != want[index] {
			t.Errorf("rule %d = %+v, want %+v", index, rules[index], want[index])
		}
	}
}

func TestRoutingRulesDescribeEveryKindOfCondition(t *testing.T) {
	private := true
	provider := providerWithRouting(t, &blueprint.Routing{
		Rules: []blueprint.Rule{{
			Match: blueprint.Match{
				Inbounds: []string{"in"}, Network: spec.NetworkTCP, Users: []string{"alice"},
				SniffedProtocols:       []string{"tls"},
				SniffedRequestHeaders:  map[string]*regexp.Regexp{"user-agent": regexp.MustCompile("curl"), ":method": regexp.MustCompile("GET")},
				SniffedBrowserFamilies: []string{"chromium"},
				Domains:                []string{"exact.example"}, DomainKeywords: []string{"ads"},
				DomainRegexes: []*regexp.Regexp{regexp.MustCompile(`^cdn\d+\.`)},
				IPs:           []netip.Prefix{netip.MustParsePrefix("192.0.2.1/32"), netip.MustParsePrefix("2001:db8::/32")},
				IPIsPrivate:   &private, IPVersions: []int{4, 6},
				Ports:     []blueprint.PortRange{{First: 80, Last: 80}, {First: 8000, Last: 9000}},
				SourceIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, SourceIPIsPrivate: &private,
				SourcePorts:       []blueprint.PortRange{{First: 1024, Last: 65535}},
				InboundLocalIPs:   []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
				InboundLocalPorts: []blueprint.PortRange{{First: 1080, Last: 1080}},
				DomainDatasets:    []string{"ir-domains"}, IPDatasets: []string{"ir-ips"}, SourceIPDatasets: []string{"lan"},
			},
			Action: route("proxy"),
		}},
		DefaultAction: route("direct"),
	}, nil)

	rules, _ := provider.RoutingRules()
	want := "inbound=in network=tcp user=alice sniffed_protocol=tls " +
		"sniffed_request_header=:method~GET,user-agent~curl sniffed_browser_family=chromium " +
		`domain=exact.example domain_keyword=ads domain_regex=^cdn\d+\. ip=192.0.2.1,2001:db8::/32 ip_is_private=true ip_version=4,6 ` +
		"port=80,8000-9000 source_ip=203.0.113.0/24 source_ip_is_private=true source_port=1024-65535 " +
		"inbound_local_ip=127.0.0.1 inbound_local_port=1080 domain_in_dataset=ir-domains ip_in_dataset=ir-ips source_ip_in_dataset=lan"
	if rules[0].Match != want {
		t.Errorf("match =\n%s\nwant\n%s", rules[0].Match, want)
	}
}

func TestRoutingRulesDescribeAnyAllAndNot(t *testing.T) {
	provider := providerWithRouting(t, &blueprint.Routing{
		Rules: []blueprint.Rule{{
			Match: blueprint.Match{
				Network: spec.NetworkTCP,
				Any:     []blueprint.Match{{Domains: []string{"a.example"}}, {Ports: []blueprint.PortRange{{First: 22, Last: 22}}}},
				All:     []blueprint.Match{{Users: []string{"bob"}}, {Inbounds: []string{"in"}}},
				Not:     &blueprint.Match{DomainSuffixes: []string{"ir"}},
			},
			Action: route("proxy"),
		}},
		DefaultAction: blueprint.Action{Block: true},
	}, nil)

	rules, _ := provider.RoutingRules()
	want := "network=tcp any(domain=a.example | port=22) all(user=bob | inbound=in) not(domain_suffix=ir)"
	if rules[0].Match != want {
		t.Errorf("match = %q, want %q", rules[0].Match, want)
	}
	if last := rules[len(rules)-1]; last.Location != "default" || last.Action != spec.APIRoutingRuleActionBlock || last.OutboundTag != "" {
		t.Errorf("default = %+v, want a block", last)
	}
}

func TestRoutingRulesWithoutARoutingSectionHaveOnlyTheDefaultTheFirstOutbound(t *testing.T) {
	provider, err := apicoreprovider.New(apicoreprovider.Parts{
		Blueprint: &blueprint.Blueprint{Outbounds: []blueprint.Outbound{{Tag: "first"}}},
		Outbounds: []component.Outbound{fixedAddressOutbound{tag: "first"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rules, _ := provider.RoutingRules()
	if len(rules) != 1 || rules[0].Location != "default" || rules[0].OutboundTag != "first" || rules[0].Action != spec.APIRoutingRuleActionRoute {
		t.Errorf("rules = %+v, want only the default to the first outbound", rules)
	}
}

func TestRoutingRuleHitsComeFromTheStatisticsAndAreMarkedWhenThereAreNone(t *testing.T) {
	routing := &blueprint.Routing{
		Rules:         []blueprint.Rule{{Match: blueprint.Match{Domains: []string{"a.example"}}, Action: route("proxy")}},
		DefaultAction: route("direct"),
	}
	registry := stats.NewRegistry(stats.Config{RuleLocations: []string{"routing.rules[0]", "default"}})
	registry.RuleHit("routing.rules[0]")
	registry.RuleHit("routing.rules[0]")
	registry.RuleHit("default")

	counted, _ := providerWithRouting(t, routing, registry).RoutingRules()
	if !counted[0].HitsCounted || counted[0].Hits != 2 || !counted[1].HitsCounted || counted[1].Hits != 1 {
		t.Errorf("counted = %+v, want 2 hits and 1 hit, both counted", counted)
	}

	uncounted, _ := providerWithRouting(t, routing, nil).RoutingRules()
	if uncounted[0].HitsCounted || uncounted[1].HitsCounted {
		t.Errorf("uncounted = %+v, want hits marked as not counted", uncounted)
	}
}
