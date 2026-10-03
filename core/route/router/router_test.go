package router

import (
	"context"
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/route/dataset"
	"github.com/The-Apranik/Apranik/core/route/match"
	"github.com/The-Apranik/Apranik/spec"
)

type fakeOutbound struct{ tag string }

func (o fakeOutbound) Tag() string { return o.tag }
func (o fakeOutbound) Networks() []spec.Network {
	return []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
}
func (o fakeOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return nil, nil
}
func (o fakeOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return nil, nil
}
func (o fakeOutbound) Close() error { return nil }

// Answers Sniff with a fixed result, and counts the calls.
type fakeInformant struct {
	metadata   spec.Metadata
	sniffed    *spec.SniffResult
	sniffCalls int
}

func (f *fakeInformant) Metadata() spec.Metadata { return f.metadata }

func (f *fakeInformant) Sniff(context.Context, []spec.Sniffer, time.Duration) *spec.SniffResult {
	f.sniffCalls++
	return f.sniffed
}

type fakeSnifferModule struct{}

func (fakeSnifferModule) Validate() error                   { return nil }
func (fakeSnifferModule) Warnings() []string                { return nil }
func (fakeSnifferModule) Info() spec.ModuleInfo             { return spec.ModuleInfo{Name: "fake"} }
func (fakeSnifferModule) NewSniffer() (spec.Sniffer, error) { return nil, nil }

func route(tag string) blueprint.Action {
	return blueprint.Action{Route: &blueprint.RouteAction{Outbound: tag}}
}

var (
	block = blueprint.Action{Block: true}
	sniff = blueprint.Action{Sniff: &blueprint.SniffAction{Sniffers: []spec.SnifferModule{fakeSnifferModule{}}}}
)

func outbounds(tags ...string) map[string]component.Outbound {
	all := make(map[string]component.Outbound, len(tags))
	for _, tag := range tags {
		all[tag] = fakeOutbound{tag}
	}
	return all
}

func testDatasets() map[string]*dataset.Dataset {
	return map[string]*dataset.Dataset{
		"ir-domains": {Kind: blueprint.DatasetKindDomain, Domains: match.NewDomainMatcher(match.DomainList{Suffixes: []string{"ir"}})},
		"ir-ips":     {Kind: blueprint.DatasetKindIP, IPs: match.NewPrefixMatcher([]netip.Prefix{netip.MustParsePrefix("185.143.232.0/22")})},
		"lan":        {Kind: blueprint.DatasetKindIP, IPs: match.NewPrefixMatcher([]netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")})},
	}
}

func mustRouter(t testing.TB, routing blueprint.Routing) *Router {
	t.Helper()
	router, err := New(Config{Routing: routing, Outbounds: outbounds("proxy", "direct", "other"), Datasets: testDatasets()})
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func toDomain(domain string, port uint16) spec.Destination {
	destination, _ := spec.NewDestinationFromDomain(domain, port)
	return destination
}

func toIP(address string, port uint16) spec.Destination {
	destination, _ := spec.NewDestinationFromIP(netip.MustParseAddr(address), port)
	return destination
}

func from(address string, port uint16) spec.Endpoint {
	endpoint, _ := spec.NewEndpoint(netip.MustParseAddr(address), port)
	return endpoint
}

func tcpTo(destination spec.Destination) spec.Metadata {
	return spec.Metadata{Network: spec.NetworkTCP, Destination: destination, Source: from("192.168.1.50", 50000), InboundTag: "mixed-in"}
}

func decide(t *testing.T, router *Router, informant *fakeInformant) string {
	t.Helper()
	decision, err := router.Route(context.Background(), informant)
	if err != nil {
		t.Fatal(err)
	}
	if decision.IsBlock() {
		return "block"
	}
	return decision.Outbound.Tag()
}

func TestEachFieldMatches(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name     string
		match    blueprint.Match
		metadata spec.Metadata
		want     bool
	}{
		{"inbound", blueprint.Match{Inbounds: []string{"mixed-in"}}, tcpTo(toDomain("a.com", 443)), true},
		{"other inbound", blueprint.Match{Inbounds: []string{"socks-in"}}, tcpTo(toDomain("a.com", 443)), false},
		{"network", blueprint.Match{Network: spec.NetworkTCP}, tcpTo(toDomain("a.com", 443)), true},
		{"other network", blueprint.Match{Network: spec.NetworkUDP}, tcpTo(toDomain("a.com", 443)), false},
		{"domain exact", blueprint.Match{Domains: []string{"a.com"}}, tcpTo(toDomain("A.com.", 443)), true},
		{"domain exact, subdomain", blueprint.Match{Domains: []string{"a.com"}}, tcpTo(toDomain("b.a.com", 443)), false},
		{"domain suffix", blueprint.Match{DomainSuffixes: []string{"ir"}}, tcpTo(toDomain("digikala.ir", 443)), true},
		{"domain keyword", blueprint.Match{DomainKeywords: []string{"kala"}}, tcpTo(toDomain("digikala.ir", 443)), true},
		{"domain regex", blueprint.Match{DomainRegexes: []*regexp.Regexp{regexp.MustCompile(`^digi`)}}, tcpTo(toDomain("digikala.ir", 443)), true},
		{"domain field, IP destination", blueprint.Match{DomainSuffixes: []string{"ir"}}, tcpTo(toIP("185.143.233.2", 443)), false},
		{"domain dataset", blueprint.Match{DomainDatasets: []string{"ir-domains"}}, tcpTo(toDomain("digikala.ir", 443)), true},
		{"ip", blueprint.Match{IPs: []netip.Prefix{netip.MustParsePrefix("185.143.232.0/22")}}, tcpTo(toIP("185.143.233.2", 443)), true},
		{"ip field, domain destination", blueprint.Match{IPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}}, tcpTo(toDomain("a.com", 443)), false},
		{"ip dataset", blueprint.Match{IPDatasets: []string{"ir-ips"}}, tcpTo(toIP("185.143.233.2", 443)), true},
		{"ip is private", blueprint.Match{IPIsPrivate: &yes}, tcpTo(toIP("10.10.34.35", 80)), true},
		{"ip is private, public", blueprint.Match{IPIsPrivate: &yes}, tcpTo(toIP("8.8.8.8", 80)), false},
		{"ip is not private", blueprint.Match{IPIsPrivate: &no}, tcpTo(toIP("8.8.8.8", 80)), true},
		{"ip is private, domain", blueprint.Match{IPIsPrivate: &yes}, tcpTo(toDomain("localhost", 80)), false},
		{"port", blueprint.Match{Ports: portRange(443, 443)}, tcpTo(toDomain("a.com", 443)), true},
		{"port range", blueprint.Match{Ports: portRange(1000, 2000)}, tcpTo(toDomain("a.com", 443)), false},
		{"source ip", blueprint.Match{SourceIPs: []netip.Prefix{netip.MustParsePrefix("192.168.1.50/32")}}, tcpTo(toDomain("a.com", 443)), true},
		{"source ip dataset", blueprint.Match{SourceIPDatasets: []string{"lan"}}, tcpTo(toDomain("a.com", 443)), true},
		{"source port", blueprint.Match{SourcePorts: portRange(49152, 65535)}, tcpTo(toDomain("a.com", 443)), true},
		{"source port range", blueprint.Match{SourcePorts: portRange(40000, 49999)}, tcpTo(toDomain("a.com", 443)), false},
		{"source ip is private", blueprint.Match{SourceIPIsPrivate: &yes}, tcpTo(toDomain("a.com", 443)), true},
		{"source ip is not private", blueprint.Match{SourceIPIsPrivate: &no}, tcpTo(toDomain("a.com", 443)), false},
		{"source ip is private, public source", blueprint.Match{SourceIPIsPrivate: &yes}, withSource(tcpTo(toDomain("a.com", 443)), from("8.8.8.8", 50000)), false},
		{"ip version 4", blueprint.Match{IPVersions: []int{4}}, tcpTo(toIP("8.8.8.8", 443)), true},
		{"ip version 4, mapped", blueprint.Match{IPVersions: []int{4}}, tcpTo(toIP("::ffff:8.8.8.8", 443)), true},
		{"ip version 6", blueprint.Match{IPVersions: []int{6}}, tcpTo(toIP("2001:db8::1", 443)), true},
		{"ip version 6, v4 destination", blueprint.Match{IPVersions: []int{6}}, tcpTo(toIP("8.8.8.8", 443)), false},
		{"ip version, domain destination", blueprint.Match{IPVersions: []int{4, 6}}, tcpTo(toDomain("a.com", 443)), false},
		{"inbound local ip", blueprint.Match{InboundLocalIPs: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}}, withLocal(tcpTo(toDomain("a.com", 443)), from("198.51.100.20", 8443)), true},
		{"inbound local ip, other", blueprint.Match{InboundLocalIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}}, withLocal(tcpTo(toDomain("a.com", 443)), from("198.51.100.20", 8443)), false},
		{"inbound local ip, unknown", blueprint.Match{InboundLocalIPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}}, tcpTo(toDomain("a.com", 443)), false},
		{"inbound local port", blueprint.Match{InboundLocalPorts: portRange(8443, 8443)}, withLocal(tcpTo(toDomain("a.com", 443)), from("198.51.100.20", 8443)), true},
		{"inbound local port, other", blueprint.Match{InboundLocalPorts: portRange(1080, 1080)}, withLocal(tcpTo(toDomain("a.com", 443)), from("198.51.100.20", 8443)), false},
		{"inbound local port, unknown", blueprint.Match{InboundLocalPorts: portRange(0, 65535)}, tcpTo(toDomain("a.com", 443)), false},
		{"request header", blueprint.Match{SniffedRequestHeaders: headers("user-agent", "^curl/", ":method", "^GET$")}, withSniffed(tcpTo(toDomain("a.com", 80)), curlRequest), true},
		{"request header, value differs", blueprint.Match{SniffedRequestHeaders: headers("user-agent", "^Mozilla/")}, withSniffed(tcpTo(toDomain("a.com", 80)), curlRequest), false},
		{"request header, absent", blueprint.Match{SniffedRequestHeaders: headers("cookie", "")}, withSniffed(tcpTo(toDomain("a.com", 80)), curlRequest), false},
		{"request header, not sniffed", blueprint.Match{SniffedRequestHeaders: headers("user-agent", "")}, tcpTo(toDomain("a.com", 80)), false},
		{"browser family", blueprint.Match{SniffedBrowserFamilies: []string{"firefox", "chromium"}}, withSniffed(tcpTo(toDomain("a.com", 443)), &spec.SniffResult{Protocol: "tls", BrowserFamily: "chromium"}), true},
		{"browser family, other", blueprint.Match{SniffedBrowserFamilies: []string{"firefox"}}, withSniffed(tcpTo(toDomain("a.com", 443)), &spec.SniffResult{Protocol: "tls", BrowserFamily: "safari"}), false},
		{"browser family, unknown", blueprint.Match{SniffedBrowserFamilies: []string{""}}, withSniffed(tcpTo(toDomain("a.com", 443)), &spec.SniffResult{Protocol: "tls"}), false},
		{"user", blueprint.Match{Users: []string{"alice"}}, withUser(tcpTo(toDomain("a.com", 443)), "alice"), true},
		{"user, no user", blueprint.Match{Users: []string{"alice"}}, tcpTo(toDomain("a.com", 443)), false},
		{"fields AND", blueprint.Match{Network: spec.NetworkTCP, Ports: portRange(80, 80)}, tcpTo(toDomain("a.com", 443)), false},
		{"empty match", blueprint.Match{}, tcpTo(toDomain("a.com", 443)), true},
		{"any", blueprint.Match{Any: []blueprint.Match{{Ports: portRange(80, 80)}, {DomainSuffixes: []string{"com"}}}}, tcpTo(toDomain("a.com", 443)), true},
		{"any, none", blueprint.Match{Any: []blueprint.Match{{Ports: portRange(80, 80)}}}, tcpTo(toDomain("a.com", 443)), false},
		{"all", blueprint.Match{All: []blueprint.Match{{Network: spec.NetworkTCP}, {Ports: portRange(443, 443)}}}, tcpTo(toDomain("a.com", 443)), true},
		{"not", blueprint.Match{Not: &blueprint.Match{DomainSuffixes: []string{"ir"}}}, tcpTo(toDomain("a.com", 443)), true},
		{"nested", blueprint.Match{Any: []blueprint.Match{{Not: &blueprint.Match{Network: spec.NetworkTCP}}, {All: []blueprint.Match{{Ports: portRange(443, 443)}}}}}, tcpTo(toDomain("a.com", 443)), true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			router := mustRouter(t, blueprint.Routing{
				Rules:         []blueprint.Rule{{Match: testCase.match, Action: route("direct")}},
				DefaultAction: route("proxy"),
			})
			got := decide(t, router, &fakeInformant{metadata: testCase.metadata}) == "direct"
			if got != testCase.want {
				t.Fatalf("matched = %v, want %v", got, testCase.want)
			}
		})
	}
}

var curlRequest = &spec.SniffResult{Protocol: "http", RequestHeaders: map[string]string{":method": "GET", ":path": "/", "user-agent": "curl/8.9.1"}}

func headers(nameThenPattern ...string) map[string]*regexp.Regexp {
	compiled := make(map[string]*regexp.Regexp)
	for index := 0; index < len(nameThenPattern); index += 2 {
		compiled[nameThenPattern[index]] = regexp.MustCompile(nameThenPattern[index+1])
	}
	return compiled
}

func withSource(metadata spec.Metadata, source spec.Endpoint) spec.Metadata {
	metadata.Source = source
	return metadata
}

func withLocal(metadata spec.Metadata, local spec.Endpoint) spec.Metadata {
	metadata.InboundLocalEndpoint = local
	return metadata
}

func withSniffed(metadata spec.Metadata, sniffed *spec.SniffResult) spec.Metadata {
	metadata.Sniffed = sniffed
	return metadata
}

func withUser(metadata spec.Metadata, user string) spec.Metadata {
	metadata.User = user
	return metadata
}

func TestFirstDecidingRuleWins(t *testing.T) {
	router := mustRouter(t, blueprint.Routing{
		Rules: []blueprint.Rule{
			{Match: blueprint.Match{DomainSuffixes: []string{"ads.ir"}}, Action: block},
			{Match: blueprint.Match{DomainSuffixes: []string{"ir"}}, Action: route("direct")},
			{Match: blueprint.Match{DomainSuffixes: []string{"digikala.ir"}}, Action: route("other")},
		},
		DefaultAction: route("proxy"),
	})
	for destination, want := range map[string]string{
		"x.ads.ir":    "block",
		"digikala.ir": "direct",
		"google.com":  "proxy",
	} {
		if got := decide(t, router, &fakeInformant{metadata: tcpTo(toDomain(destination, 443))}); got != want {
			t.Errorf("%s: got %s, want %s", destination, got, want)
		}
	}
}

func TestDecisionNamesTheRule(t *testing.T) {
	router := mustRouter(t, blueprint.Routing{
		Rules:         []blueprint.Rule{{Match: blueprint.Match{Ports: portRange(80, 80)}, Action: route("direct")}, {Match: blueprint.Match{}, Action: block}},
		DefaultAction: route("proxy"),
	})
	decision, _ := router.Route(context.Background(), &fakeInformant{metadata: tcpTo(toDomain("a.com", 443))})
	if decision.Rule != "routing.rules[1]" || !decision.IsBlock() {
		t.Fatalf("got %+v, want a block by routing.rules[1]", decision)
	}
}

func TestDefaultBlock(t *testing.T) {
	router := mustRouter(t, blueprint.Routing{DefaultAction: block})
	if got := decide(t, router, &fakeInformant{metadata: tcpTo(toDomain("a.com", 443))}); got != "block" {
		t.Fatalf("got %s, want block", got)
	}
}

func TestSniffRunsOnlyWhenReachedAndFeedsLaterRules(t *testing.T) {
	router := mustRouter(t, blueprint.Routing{
		Rules: []blueprint.Rule{
			{Match: blueprint.Match{SniffedProtocols: []string{"tls"}}, Action: route("other")}, // before sniff: sees nothing
			{Match: blueprint.Match{Ports: portRange(22, 22)}, Action: route("direct")},
			{Match: blueprint.Match{Network: spec.NetworkTCP}, Action: sniff},
			{Match: blueprint.Match{DomainSuffixes: []string{"ir"}}, Action: route("direct")},
		},
		DefaultAction: route("proxy"),
	})

	ssh := &fakeInformant{metadata: tcpTo(toIP("185.143.233.2", 22))}
	if got := decide(t, router, ssh); got != "direct" || ssh.sniffCalls != 0 {
		t.Errorf("ssh: got %s after %d sniffs, want direct with none", got, ssh.sniffCalls)
	}

	byIP := &fakeInformant{metadata: tcpTo(toIP("185.143.233.2", 443)), sniffed: &spec.SniffResult{Protocol: "tls", Domain: "Digikala.IR"}}
	if got := decide(t, router, byIP); got != "direct" || byIP.sniffCalls != 1 {
		t.Errorf("sniffed .ir: got %s after %d sniffs, want direct after one", got, byIP.sniffCalls)
	}

	nothing := &fakeInformant{metadata: tcpTo(toIP("8.8.8.8", 443))}
	if got := decide(t, router, nothing); got != "proxy" {
		t.Errorf("nothing sniffed: got %s, want proxy", got)
	}
}

// Like sing-box and Xray's routeOnly: the sniffed name wins when there is one.
func TestDomainRulesReadTheSniffedNameFirst(t *testing.T) {
	router := mustRouter(t, blueprint.Routing{
		Rules: []blueprint.Rule{
			{Match: blueprint.Match{}, Action: sniff},
			{Match: blueprint.Match{DomainSuffixes: []string{"ir"}}, Action: route("direct")},
		},
		DefaultAction: route("proxy"),
	})
	disagreeing := &fakeInformant{metadata: tcpTo(toDomain("google.com", 443)), sniffed: &spec.SniffResult{Protocol: "tls", Domain: "digikala.ir"}}
	if got := decide(t, router, disagreeing); got != "direct" {
		t.Errorf("got %s, want direct: the sniffed name decides", got)
	}
	emptySniff := &fakeInformant{metadata: tcpTo(toDomain("digikala.ir", 443)), sniffed: &spec.SniffResult{Protocol: "http"}}
	if got := decide(t, router, emptySniff); got != "direct" {
		t.Errorf("got %s, want direct: no sniffed name falls back to the destination", got)
	}
}

func TestNewWithDefaultOutbound(t *testing.T) {
	router := NewWithDefaultOutbound(fakeOutbound{"only"})
	if got := decide(t, router, &fakeInformant{metadata: tcpTo(toDomain("a.com", 443))}); got != "only" {
		t.Fatalf("got %s, want only", got)
	}
}

func TestNewRefusesWhatCheckWouldHave(t *testing.T) {
	_, err := New(Config{
		Routing: blueprint.Routing{
			Rules: []blueprint.Rule{
				{Match: blueprint.Match{DomainDatasets: []string{"ir-ips"}}, Action: route("direct")},
				{Match: blueprint.Match{IPDatasets: []string{"absent"}}, Action: route("nowhere")},
				{Match: blueprint.Match{}, Action: blueprint.Action{Block: true, Route: &blueprint.RouteAction{Outbound: "direct"}}},
			},
			DefaultAction: sniff,
		},
		Outbounds: outbounds("direct"),
		Datasets:  testDatasets(),
	})
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"rules[0]", "holds ip values", "rules[1]", "no outbound \"nowhere\"", "rules[2]", "exactly one", "default_action"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// The router runs once per TCP connection and once per new UDP destination.
// A realistic config: sniff, block ads, private direct, Iran direct, the rest
// proxied, with dataset sizes close to the real ir and ads categories.
func BenchmarkRouteRealisticConfig(b *testing.B) {
	adsDomains := make([]string, 60_000)
	for index := range adsDomains {
		adsDomains[index] = "ads" + strings.Repeat("x", index%7) + string(rune('a'+index%26)) + ".tracker" + itoa(index) + ".com"
	}
	irDomains := make([]string, 20_000)
	for index := range irDomains {
		irDomains[index] = "site" + itoa(index) + ".ir"
	}
	datasets := map[string]*dataset.Dataset{
		"ads":        {Kind: blueprint.DatasetKindDomain, Domains: match.NewDomainMatcher(match.DomainList{Suffixes: adsDomains})},
		"ir-domains": {Kind: blueprint.DatasetKindDomain, Domains: match.NewDomainMatcher(match.DomainList{Suffixes: irDomains})},
		"ir-ips":     testDatasets()["ir-ips"],
	}
	yes := true
	router, err := New(Config{
		Routing: blueprint.Routing{
			Rules: []blueprint.Rule{
				{Match: blueprint.Match{Inbounds: []string{"mixed-in"}, Network: spec.NetworkTCP}, Action: sniff},
				{Match: blueprint.Match{DomainDatasets: []string{"ads"}}, Action: block},
				{Match: blueprint.Match{IPIsPrivate: &yes}, Action: route("direct")},
				{Match: blueprint.Match{Any: []blueprint.Match{{DomainDatasets: []string{"ir-domains"}}, {IPDatasets: []string{"ir-ips"}}, {DomainSuffixes: []string{"ir"}}}}, Action: route("direct")},
				{Match: blueprint.Match{Network: spec.NetworkUDP, Ports: portRange(443, 443)}, Action: block},
			},
			DefaultAction: route("proxy"),
		},
		Outbounds: outbounds("proxy", "direct"),
		Datasets:  datasets,
	})
	if err != nil {
		b.Fatal(err)
	}
	informant := &fakeInformant{
		metadata: tcpTo(toIP("142.250.185.78", 443)),
		sniffed:  &spec.SniffResult{Protocol: "tls", Domain: "www.youtube.com"},
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		decision, _ := router.Route(ctx, informant)
		if decision.Outbound == nil || decision.Outbound.Tag() != "proxy" {
			b.Fatal("wrong decision")
		}
	}
}

func itoa(number int) string {
	if number == 0 {
		return "0"
	}
	var digits []byte
	for ; number > 0; number /= 10 {
		digits = append([]byte{byte('0' + number%10)}, digits...)
	}
	return string(digits)
}

func portRange(first, last uint16) []blueprint.PortRange {
	return []blueprint.PortRange{{First: first, Last: last}}
}
