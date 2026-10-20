package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config"
	"github.com/we-apranik/apranik/core/blueprint"
)

func withDNS(dns, routing string) string {
	document := strings.Replace(goodDocument, `"outbounds": [{
    "tag": "freedom",
    "protocol": {"type": "freedom"}
  }]`, `"outbounds": [
    {"tag": "freedom", "protocol": {"type": "freedom"}},
    {"tag": "direct", "protocol": {"type": "freedom"}},
    {"tag": "fragmented", "transport": [{"type": "tcp"}, {"type": "fragment"}], "protocol": {"type": "freedom"}},
    {"tag": "proxy", "server": "example.com", "server_port": 1080, "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}
  ]`, 1)
	document = strings.TrimSuffix(strings.TrimSpace(document), "}") + `, "dns": ` + dns
	if routing != "" {
		document += `, "routing": ` + routing
	}
	return document + `}`
}

const everyDNSField = `{
  "ip_versions": "prefer_ipv6",
  "cache": {"max_entries": 100, "per_server": true, "serve_stale": {"enabled": true, "max_stale_age": "10m"}},
  "servers": [
    {"tag": "google", "type": "doh", "settings": {"server": "8.8.8.8", "host": "dns.google", "timeout": "8s"},
     "tls": {"server_name": "dns.google", "alpn": ["http/1.1"]}, "outbound": "proxy"},
    {"tag": "cf", "type": "dot", "settings": {"server": "1.1.1.1"}, "outbound": "fragmented"},
    {"tag": "bootstrap", "type": "udp", "settings": {"server": "192.0.2.53"}, "outbound": "direct", "ip_versions": "ipv4_only"},
    {"tag": "plain", "type": "tcp", "settings": {"server": "192.0.2.54:5353"}, "outbound": "direct"},
    {"tag": "os", "type": "system", "settings": {"timeout": "3s"}}
  ],
  "rules": [{"match": {"domain_suffix": ["ir"]}, "server": "bootstrap"}],
  "default_server": "google"
}`

func TestDNSSectionDecodes(t *testing.T) {
	document := strings.Replace(withDNS(everyDNSField, ""), `"protocol": {"type": "socks5"}}`, `"protocol": {"type": "socks5"}, "dns_server": "bootstrap"}`, 1)
	configuration, diagnostics := config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))

	dns := configuration.DNS
	if dns.IPVersions != blueprint.IPVersionsPreferIPv6 || dns.DefaultServer != "google" {
		t.Errorf("dns = %+v", dns)
	}
	if !dns.Cache.PerServer || !dns.Cache.ServeStale || dns.Cache.MaxStaleAge == nil || *dns.Cache.MaxStaleAge != 10*time.Minute || dns.Cache.MaxEntries == nil || *dns.Cache.MaxEntries != 100 {
		t.Errorf("cache = %+v", dns.Cache)
	}
	google, cf, bootstrap, plain, system := dns.Servers[0], dns.Servers[1], dns.Servers[2], dns.Servers[3], dns.Servers[4]
	if google.Stream == nil || google.TLS == nil || google.Outbound != "proxy" {
		t.Errorf("doh server = %+v", google)
	}
	if cf.Stream == nil || cf.TLS == nil {
		t.Errorf("dot without a tls block did not get the tls defaults: %+v", cf)
	}
	if bootstrap.Datagram == nil || bootstrap.IPVersions != blueprint.IPVersionsIPv4Only {
		t.Errorf("udp server = %+v", bootstrap)
	}
	if plain.Stream == nil || plain.TLS != nil {
		t.Errorf("tcp server = %+v", plain)
	}
	if system.System == nil || system.System.Timeout == nil || *system.System.Timeout != 3*time.Second {
		t.Errorf("system server = %+v", system)
	}
	if len(dns.Rules) != 1 || dns.Rules[0].Server != "bootstrap" || dns.Rules[0].Match.DomainSuffixes[0] != "ir" {
		t.Errorf("rules = %+v", dns.Rules)
	}
	if configuration.Outbounds[3].DNSServer != "bootstrap" {
		t.Errorf("outbound dns_server = %q", configuration.Outbounds[3].DNSServer)
	}
}

func TestDNSRefusals(t *testing.T) {
	server := func(fields string) string {
		return `{"servers": [` + fields + `], "default_server": "a"}`
	}
	cases := []struct{ name, dns, routing, location, message string }{
		{"no servers", `{"default_server": "a"}`, "", "dns.servers", "at least one server"},
		{"no default", `{"servers": [{"tag": "a", "type": "system"}]}`, "", "dns.default_server", "needs a default_server"},
		{"unknown default", `{"servers": [{"tag": "a", "type": "system"}], "default_server": "b"}`, "", "dns.default_server", `"b"`},
		{"missing type", server(`{"tag": "a"}`), "", "dns.servers[0]", `"type"`},
		{"unknown type", server(`{"tag": "a", "type": "doq"}`), "", "dns.servers[0].type", "doq"},
		{"missing server address", server(`{"tag": "a", "type": "udp"}`), "", "dns.servers[0].settings", "server is required"},
		{"duplicate tag", `{"servers": [{"tag": "a", "type": "system"}, {"tag": "a", "type": "system"}], "default_server": "a"}`, "", "dns.servers[1].tag", "already used"},
		{"tls on udp", server(`{"tag": "a", "type": "udp", "settings": {"server": "192.0.2.1"}, "tls": {}}`), "", "dns.servers[0].tls", "takes no tls"},
		{"tls on tcp", server(`{"tag": "a", "type": "tcp", "settings": {"server": "192.0.2.1"}, "tls": {"server_name": "x"}}`), "", "dns.servers[0].tls", "takes no tls"},
		{"doh offering h2", server(`{"tag": "a", "type": "doh", "settings": {"server": "8.8.8.8"}, "tls": {"alpn": ["h2"]}}`), "", "dns.servers[0].tls.alpn", `set alpn to ["http/1.1"]`},
		{"udp over a proxy without udp", server(`{"tag": "a", "type": "udp", "settings": {"server": "192.0.2.1"}, "outbound": "proxy"}`), "", "dns.servers[0].outbound", "cannot carry UDP"},
		{"unknown outbound", server(`{"tag": "a", "type": "tcp", "settings": {"server": "192.0.2.1"}, "outbound": "nowhere"}`), "", "dns.servers[0].outbound", `"nowhere"`},
		{"system with an outbound", server(`{"tag": "a", "type": "system", "outbound": "direct"}`), "", "dns.servers[0].outbound", "takes no outbound"},
		{"bad ip_versions", server(`{"tag": "a", "type": "system", "ip_versions": "ipv5"}`), "", "dns.servers[0].ip_versions", "ipv5"},
		{"unknown dns_server", server(`{"tag": "a", "type": "tcp", "settings": {"server": "dns.example"}, "dns_server": "b"}`), "", "dns.servers[0].dns_server", `"b"`},
		{"rule on a port", `{"servers": [{"tag": "a", "type": "system"}], "rules": [{"match": {"port": [53]}, "server": "a"}], "default_server": "a"}`, "", "dns.rules[0].match", "only on the name"},
		{"rule on a port, nested", `{"servers": [{"tag": "a", "type": "system"}], "rules": [{"match": {"any": [{"ip": ["1.1.1.1"]}]}, "server": "a"}], "default_server": "a"}`, "", "dns.rules[0].match", "only on the name"},
		{"rule without server", `{"servers": [{"tag": "a", "type": "system"}], "rules": [{"match": {"domain": ["x"]}}], "default_server": "a"}`, "", "dns.rules[0]", `"server"`},
		{"rule to an unknown server", `{"servers": [{"tag": "a", "type": "system"}], "rules": [{"match": {"domain": ["x"]}, "server": "b"}], "default_server": "a"}`, "", "dns.rules[0].server", `"b"`},
		{"rule with an unknown dataset", `{"servers": [{"tag": "a", "type": "system"}], "rules": [{"match": {"domain_in_dataset": ["ir"]}, "server": "a"}], "default_server": "a"}`, "", "dns.rules[0].match.domain_in_dataset[0]", `"ir"`},
		{"serve_stale without a cache", `{"servers": [{"tag": "a", "type": "system"}], "cache": {"disable": true, "serve_stale": {"enabled": true}}, "default_server": "a"}`, "", "dns.cache.serve_stale", "cache is disabled"},
		{"negative max_entries", `{"servers": [{"tag": "a", "type": "system"}], "cache": {"max_entries": -1}, "default_server": "a"}`, "", "dns.cache.max_entries", "greater than zero"},
		{"zero system timeout", `{"servers": [{"tag": "a", "type": "system", "settings": {"timeout": "0s"}}], "default_server": "a"}`, "", "dns.servers[0].settings.timeout", "greater than zero"},
		{"zero max_stale_age", `{"servers": [{"tag": "a", "type": "system"}], "cache": {"serve_stale": {"enabled": true, "max_stale_age": "0s"}}, "default_server": "a"}`, "", "dns.cache.serve_stale.max_stale_age", "greater than zero"},
		{"zero max_entries", `{"servers": [{"tag": "a", "type": "system"}], "cache": {"max_entries": 0}, "default_server": "a"}`, "", "dns.cache.max_entries", "greater than zero"},
		{"bad global ip_versions", `{"servers": [{"tag": "a", "type": "system"}], "ip_versions": "both", "default_server": "a"}`, "", "dns.ip_versions", "both"},
		{"resolve with an unknown server", `{"servers": [{"tag": "a", "type": "system"}], "default_server": "a"}`,
			`{"rules": [{"action": {"type": "resolve", "settings": {"server": "b"}}}, {"match": {"ip": ["10.0.0.0/8"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`,
			"routing.rules[0].action.settings.server", `"b"`},
		{"resolve as the default", `{"servers": [{"tag": "a", "type": "system"}], "default_server": "a"}`,
			`{"default_action": {"type": "resolve"}}`, "routing.default_action", "must route or block"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, withDNS(testCase.dns, testCase.routing))
			if !failed || !find(diagnostics, testCase.location, testCase.message) {
				t.Errorf("want an error at %s containing %q; got:\n%v", testCase.location, testCase.message, diagnostics)
			}
		})
	}
}

func TestDOHGetsItsApplicationProtocolsWhenTheUserWritesNone(t *testing.T) {
	for name, tls := range map[string]string{"no tls block": ``, "tls block without alpn": `, "tls": {"server_name": "dns.google"}`} {
		t.Run(name, func(t *testing.T) {
			dns := `{"servers": [{"tag": "g", "type": "doh", "settings": {"server": "8.8.8.8"}` + tls + `}], "default_server": "g"}`
			if diagnostics, failed := loadAndCheck(t, withDNS(dns, "")); failed {
				t.Fatalf("doh without alpn was refused:\n%v", diagnostics)
			}
		})
	}
}

func TestDNSLoopIsRefusedAndNamed(t *testing.T) {
	dns := `{"servers": [{"tag": "google", "type": "doh", "settings": {"server": "8.8.8.8"}, "tls": {"alpn": ["http/1.1"]}, "outbound": "proxy"}], "default_server": "google"}`
	diagnostics, failed := loadAndCheck(t, withDNS(dns, ""))
	want := `dns server "google" → outbound "proxy" → dns server "google"`
	if !failed || !find(diagnostics, "dns.servers[0]", want) {
		t.Errorf("want the loop %s reported; got:\n%v", want, diagnostics)
	}

	broken := strings.Replace(withDNS(strings.Replace(dns, `"outbound": "proxy"}]`, `"outbound": "proxy"}, {"tag": "os", "type": "system"}]`, 1), ""),
		`"protocol": {"type": "socks5"}}`, `"protocol": {"type": "socks5"}, "dns_server": "os"}`, 1)
	diagnostics, failed = loadAndCheck(t, broken)
	if failed {
		t.Errorf("a dns_server off the loop did not break it: %v", diagnostics.Err())
	}
}

func TestOutboundDNSServerRefusals(t *testing.T) {
	dns := `{"servers": [{"tag": "os", "type": "system"}], "default_server": "os"}`
	freedom := strings.Replace(withDNS(dns, ""), `{"tag": "direct", "protocol": {"type": "freedom"}}`, `{"tag": "direct", "protocol": {"type": "freedom"}, "dns_server": "os"}`, 1)
	if diagnostics, failed := loadAndCheck(t, freedom); !failed || !find(diagnostics, "outbounds[1].dns_server", "no server name of its own") {
		t.Errorf("freedom's dns_server was accepted: %v", diagnostics)
	}
	unknown := strings.Replace(withDNS(dns, ""), `"protocol": {"type": "socks5"}}`, `"protocol": {"type": "socks5"}, "dns_server": "nope"}`, 1)
	if diagnostics, failed := loadAndCheck(t, unknown); !failed || !find(diagnostics, "outbounds[3].dns_server", `"nope"`) {
		t.Errorf("an unknown dns_server was accepted: %v", diagnostics)
	}
	reserved := strings.Replace(withDNS(dns, ""), `{"tag": "direct",`, `{"tag": "dns-default-freedom",`, 1)
	if diagnostics, failed := loadAndCheck(t, reserved); !failed || !find(diagnostics, "outbounds[1].tag", "reserved") {
		t.Errorf("the reserved tag was accepted: %v", diagnostics)
	}
}

func TestDNSWarnings(t *testing.T) {
	cases := []struct{ name, dns, routing, location, message string }{
		{"direct server beside a proxy",
			`{"servers": [{"tag": "a", "type": "udp", "settings": {"server": "192.0.2.1"}}], "default_server": "a"}`,
			`{"default_action": {"type": "route", "settings": {"outbound": "proxy"}}}`,
			"dns.servers[0].outbound", "show the network which names"},
		{"udp under a fragmenting freedom",
			`{"servers": [{"tag": "a", "type": "udp", "settings": {"server": "192.0.2.1"}, "outbound": "fragmented"}], "default_server": "a"}`,
			"", "dns.servers[0].outbound", "only shapes TCP"},
		{"resolve nobody reads",
			`{"servers": [{"tag": "a", "type": "system"}], "default_server": "a"}`,
			`{"rules": [{"action": {"type": "resolve"}}, {"match": {"port": [22]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`,
			"routing.rules[0].action", "only delays"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, withDNS(testCase.dns, testCase.routing))
			if failed {
				t.Fatalf("unexpected errors: %v", diagnostics.Err())
			}
			if !find(diagnostics, testCase.location, testCase.message) {
				t.Errorf("want a warning at %s containing %q; got:\n%v", testCase.location, testCase.message, diagnostics)
			}
		})
	}
}
