package config_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/route/dataset/geodata/geodatatest"
	"github.com/The-Apranik/Apranik/spec"
)

// goodDocument with two outbounds and the given routing section.
func withRouting(routing string) string {
	document := strings.Replace(goodDocument, `"outbounds": [{
    "tag": "freedom",
    "protocol": {"type": "freedom"}
  }]`, `"outbounds": [
    {"tag": "freedom", "protocol": {"type": "freedom"}},
    {"tag": "direct", "protocol": {"type": "freedom"}},
    {"tag": "proxy", "server": "example.com", "server_port": 1080, "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}
  ]`, 1)
	return strings.TrimSuffix(strings.TrimSpace(document), "}") + `, "routing": ` + routing + `}`
}

const everyField = `{
  "rules": [
    {"match": {"inbound": ["socks-in"], "network": "tcp"},
     "action": {"type": "sniff", "settings": {"sniffers": [{"type": "tls"}, {"type": "http"}], "timeout": "250ms"}}},
    {"match": {"domain": ["Exact.Example.COM."], "domain_suffix": [".ir"], "domain_keyword": ["Kala"], "domain_regex": ["^ads\\d+\\."]},
     "action": {"type": "block"}},
    {"match": {"ip": ["10.0.0.1", "192.168.0.0/16", "::ffff:1.2.3.4"], "ip_is_private": true, "port": [443], "port_range": ["1000-2000"]},
     "action": {"type": "route", "settings": {"outbound": "direct"}}},
    {"match": {"source_ip": ["192.168.1.50"], "source_port": [5353], "user": ["alice"], "sniffed_protocol": ["tls"]},
     "action": {"type": "route", "settings": {"outbound": "proxy"}}},
    {"match": {"any": [{"network": "udp"}, {"all": [{"port": [53]}]}], "not": {"port": [22]}},
     "action": {"type": "block"}}
  ],
  "default_action": {"type": "route", "settings": {"outbound": "freedom"}}
}`

func TestEveryRoutingFieldDecodes(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(withRouting(everyField)), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))

	routing := configuration.Routing
	if routing == nil || len(routing.Rules) != 5 {
		t.Fatalf("routing = %+v, want five rules", routing)
	}

	sniff := routing.Rules[0].Action.Sniff
	if sniff == nil || len(sniff.Sniffers) != 2 || sniff.Timeout != 250*time.Millisecond {
		t.Errorf("sniff action = %+v", sniff)
	}
	if routing.Rules[0].Match.Network != spec.NetworkTCP || routing.Rules[0].Match.Inbounds[0] != "socks-in" {
		t.Errorf("rule 0 match = %+v", routing.Rules[0].Match)
	}

	domains := routing.Rules[1].Match
	if domains.Domains[0] != "exact.example.com" || domains.DomainSuffixes[0] != "ir" || domains.DomainKeywords[0] != "kala" || len(domains.DomainRegexes) != 1 {
		t.Errorf("domain fields = %+v, want them normalized", domains)
	}
	if !routing.Rules[1].Action.Block {
		t.Error("rule 1 does not block")
	}

	addresses := routing.Rules[2].Match
	wantIPs := []netip.Prefix{netip.MustParsePrefix("10.0.0.1/32"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("1.2.3.4/32")}
	for index, want := range wantIPs {
		if addresses.IPs[index] != want {
			t.Errorf("ip[%d] = %s, want %s", index, addresses.IPs[index], want)
		}
	}
	if addresses.IPIsPrivate == nil || !*addresses.IPIsPrivate {
		t.Error("ip_is_private not decoded")
	}
	if len(addresses.Ports) != 2 || addresses.Ports[0] != (blueprint.PortRange{First: 443, Last: 443}) || addresses.Ports[1] != (blueprint.PortRange{First: 1000, Last: 2000}) {
		t.Errorf("ports = %+v", addresses.Ports)
	}
	if routing.Rules[2].Action.Route.Outbound != "direct" {
		t.Errorf("rule 2 routes to %q", routing.Rules[2].Action.Route.Outbound)
	}

	source := routing.Rules[3].Match
	if source.SourceIPs[0] != netip.MustParsePrefix("192.168.1.50/32") || source.SourcePorts[0].First != 5353 ||
		source.Users[0] != "alice" || source.SniffedProtocols[0] != "tls" {
		t.Errorf("rule 3 match = %+v", source)
	}

	logic := routing.Rules[4].Match
	if len(logic.Any) != 2 || len(logic.Any[1].All) != 1 || logic.Not == nil || logic.Not.Ports[0].First != 22 {
		t.Errorf("rule 4 match = %+v", logic)
	}

	if routing.DefaultAction.Route == nil || routing.DefaultAction.Route.Outbound != "freedom" {
		t.Errorf("default_action = %+v", routing.DefaultAction)
	}
}

func TestRoutingRefusals(t *testing.T) {
	cases := []struct {
		name, routing, location, message string
	}{
		{"missing default", `{"rules": []}`, "routing.default_action", "needs a default_action"},
		{"sniff default", `{"default_action": {"type": "sniff", "settings": {"sniffers": [{"type": "tls"}]}}}`, "routing.default_action", "must route or block"},
		{"unknown action", `{"rules": [{"action": {"type": "reject"}}], "default_action": {"type": "block"}}`, "routing.rules[0].action.type", `unknown action "reject"`},
		{"missing action", `{"rules": [{"match": {}}], "default_action": {"type": "block"}}`, "routing.rules[0].action", `"action"`},
		{"route without outbound", `{"default_action": {"type": "route", "settings": {}}}`, "routing.default_action.settings", `"outbound"`},
		{"unknown outbound", `{"default_action": {"type": "route", "settings": {"outbound": "nowhere"}}}`, "routing.default_action.settings.outbound", `"nowhere"`},
		{"block with settings", `{"default_action": {"type": "block", "settings": {"method": "drop"}}}`, "routing.default_action.settings", "no settings"},
		{"route setting typo", `{"default_action": {"type": "route", "settings": {"outbond": "proxy"}}}`, "routing.default_action.settings", "outbond"},
		{"bad regex", `{"rules": [{"match": {"domain_regex": ["("]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.domain_regex[0]", "missing closing"},
		{"bad ip", `{"rules": [{"match": {"ip": ["300.1.1.1"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.ip[0]", "neither"},
		{"bad port range", `{"rules": [{"match": {"port_range": ["2000-1000"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.port_range[0]", "starts after"},
		{"port range without dash", `{"rules": [{"match": {"port_range": ["443"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.port_range[0]", "not a range"},
		{"bad network", `{"rules": [{"match": {"network": "sctp"}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.network", "sctp"},
		{"nested location", `{"rules": [{"match": {"any": [{}, {"not": {"ip": ["x"]}}]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.any[1].not.ip[0]", "neither"},
		{"unknown inbound", `{"rules": [{"match": {"inbound": ["ghost"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.inbound[0]", `"ghost"`},
		{"unreachable rule", `{"rules": [{"match": {}, "action": {"type": "block"}}, {"match": {"port": [80]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[1]", "can never run"},
		{"empty not", `{"rules": [{"match": {"not": {}}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.not", "matches nothing"},
		{"sniff without sniffers", `{"rules": [{"action": {"type": "sniff", "settings": {}}}], "default_action": {"type": "block"}}`, "routing.rules[0].action.settings.sniffers", "at least one sniffer"},
		{"unknown sniffer", `{"rules": [{"action": {"type": "sniff", "settings": {"sniffers": [{"type": "bittorrent"}]}}}], "default_action": {"type": "block"}}`, "routing.rules[0].action.settings.sniffers[0]", "bittorrent"},
		{"unknown dataset", `{"rules": [{"match": {"domain_in_dataset": ["ir"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.domain_in_dataset[0]", `no dataset is tagged "ir"`},
		{"dataset of the wrong kind", `{"datasets": [{"tag": "ir-ips", "source": {"file": "geoip.dat"}, "format": {"type": "geodata", "kind": "ip", "category": "ir"}}],
		  "rules": [{"match": {"domain_in_dataset": ["ir-ips"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match.domain_in_dataset[0]", "holds ip values"},
		{"dataset without kind", `{"datasets": [{"tag": "x", "source": {"file": "geosite.dat"}, "format": {"type": "geodata", "category": "ir"}}], "default_action": {"type": "block"}}`, "routing.datasets[0].format.kind", "unknown kind"},
		{"dataset unknown format", `{"datasets": [{"tag": "x", "source": {"file": "x.srs"}, "format": {"type": "srs", "kind": "domain"}}], "default_action": {"type": "block"}}`, "routing.datasets[0].format.type", `"srs"`},
		{"dataset without category", `{"datasets": [{"tag": "x", "source": {"file": "geosite.dat"}, "format": {"type": "geodata", "kind": "domain"}}], "default_action": {"type": "block"}}`, "routing.datasets[0].format", `"category"`},
		{"dataset without file", `{"datasets": [{"tag": "x", "format": {"type": "geodata", "kind": "domain", "category": "ir"}}], "default_action": {"type": "block"}}`, "routing.datasets[0].source", `"file"`},
		{"ip dataset with attribute", `{"datasets": [{"tag": "x", "source": {"file": "geoip.dat"}, "format": {"type": "geodata", "kind": "ip", "category": "ir", "attribute": "cn"}}], "default_action": {"type": "block"}}`, "routing.datasets[0].format.attribute", "domain entries"},
		{"duplicate dataset tag", `{"datasets": [
		  {"tag": "x", "source": {"file": "a.dat"}, "format": {"type": "geodata", "kind": "ip", "category": "ir"}},
		  {"tag": "x", "source": {"file": "b.dat"}, "format": {"type": "geodata", "kind": "ip", "category": "ir"}}], "default_action": {"type": "block"}}`, "routing.datasets[1].tag", "already used"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, withRouting(testCase.routing))
			if !failed || !find(diagnostics, testCase.location, testCase.message) {
				t.Errorf("want an error at %s containing %q; got:\n%v", testCase.location, testCase.message, diagnostics)
			}
		})
	}
}

func TestRoutingWarnings(t *testing.T) {
	cases := []struct {
		name, routing, location, message string
	}{
		{"sniffed protocol before sniff", `{"rules": [{"match": {"sniffed_protocol": ["tls"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].match", "never matches"},
		{"sniff nobody reads", `{"rules": [{"action": {"type": "sniff", "settings": {"sniffers": [{"type": "tls"}]}}}, {"match": {"port": [22]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].action", "only delays"},
		{"long sniff timeout", `{"rules": [{"action": {"type": "sniff", "settings": {"sniffers": [{"type": "tls"}], "timeout": "10s"}}}, {"match": {"domain_suffix": ["ir"]}, "action": {"type": "block"}}], "default_action": {"type": "block"}}`, "routing.rules[0].action.settings.timeout", "SSH"},
		{"udp to a proxy without udp", `{"rules": [{"match": {"network": "udp"}, "action": {"type": "route", "settings": {"outbound": "proxy"}}}], "default_action": {"type": "block"}}`, "routing.rules[0].action.settings.outbound", "cannot carry UDP"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, withRouting(testCase.routing))
			if failed {
				t.Fatalf("unexpected errors: %v", diagnostics.Err())
			}
			if !find(diagnostics, testCase.location, testCase.message) {
				t.Errorf("want a warning at %s containing %q; got:\n%v", testCase.location, testCase.message, diagnostics)
			}
		})
	}
}

func TestNoRoutingWithSeveralOutboundsWarns(t *testing.T) {
	document := strings.Replace(withRouting(`{"default_action": {"type": "block"}}`), `, "routing": {"default_action": {"type": "block"}}`, "", 1)
	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("unexpected errors: %v", diagnostics.Err())
	}
	if !find(diagnostics, "routing", `first outbound, "freedom"`) {
		t.Errorf("no warning that only the first outbound is used: %v", diagnostics)
	}
}

func writeDatasetFixtures(t *testing.T, directory string) {
	t.Helper()
	site := geodatatest.Site(map[string][]geodatatest.Domain{"IR": {{Type: geodatatest.Suffix, Value: "ir"}}})
	ip := geodatatest.IP(map[string][]netip.Prefix{"IR": {netip.MustParsePrefix("185.143.232.0/22")}})
	if err := os.WriteFile(filepath.Join(directory, "geosite.dat"), site, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "geoip.dat"), ip, 0o600); err != nil {
		t.Fatal(err)
	}
}

const datasetRouting = `{
  "datasets": [
    {"tag": "ir-domains", "source": {"file": "geosite.dat"}, "format": {"type": "geodata", "kind": "domain", "category": "ir"}},
    {"tag": "ir-ips", "source": {"file": "geoip.dat"}, "format": {"type": "geodata", "kind": "ip", "category": "ir"}}
  ],
  "rules": [
    {"match": {"any": [{"domain_in_dataset": ["ir-domains"]}, {"ip_in_dataset": ["ir-ips"]}]},
     "action": {"type": "route", "settings": {"outbound": "direct"}}}
  ],
  "default_action": {"type": "route", "settings": {"outbound": "freedom"}}
}`

// LoadFile resolves a dataset's file against the config file's directory and
// reads it, so a missing category is reported before run.
func TestLoadFileResolvesAndReadsDatasets(t *testing.T) {
	directory := t.TempDir()
	writeDatasetFixtures(t, directory)
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, []byte(withRouting(datasetRouting)), 0o600); err != nil {
		t.Fatal(err)
	}

	configuration, diagnostics := config.LoadFile(configPath, modules(t))
	requireNoErrors(t, diagnostics)
	if got := configuration.Routing.Datasets[0].File; got != filepath.Join(directory, "geosite.dat") {
		t.Errorf("dataset file = %q, want it resolved against the config's directory", got)
	}

	broken := strings.Replace(withRouting(datasetRouting), `"kind": "domain", "category": "ir"`, `"kind": "domain", "category": "cn"`, 1)
	if err := os.WriteFile(configPath, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	_, diagnostics = config.LoadFile(configPath, modules(t))
	if !diagnostics.HasErrors() || !find(diagnostics, "routing.datasets[0]", `"cn"`) {
		t.Errorf("a missing category was not reported at its dataset: %v", diagnostics)
	}

	swapped := strings.Replace(withRouting(datasetRouting), `"file": "geosite.dat"`, `"file": "geoip.dat"`, 1)
	if err := os.WriteFile(configPath, []byte(swapped), 0o600); err != nil {
		t.Fatal(err)
	}
	_, diagnostics = config.LoadFile(configPath, modules(t))
	if !diagnostics.HasErrors() || !find(diagnostics, "routing.datasets[0]", "holds IP ranges") {
		t.Errorf("a geoip file declared as domains was not reported: %v", diagnostics)
	}
}

func TestParseWithoutBaseDirectoryKeepsRelativePaths(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(withRouting(datasetRouting)), modules(t))
	requireNoErrors(t, diagnostics)
	if got := configuration.Routing.Datasets[0].File; got != "geosite.dat" {
		t.Errorf("dataset file = %q, want it left relative", got)
	}
}
