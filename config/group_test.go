package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/blueprint"
)

// documentWithOutbounds is a good document with the given outbounds, which
// are JSON objects separated by commas, and the given routing section (empty
// for none).
func documentWithOutbounds(outbounds, routing string) string {
	document := strings.Replace(goodDocument, `"outbounds": [{
    "tag": "freedom",
    "protocol": {"type": "freedom"}
  }]`, `"outbounds": [`+outbounds+`]`, 1)
	if routing != "" {
		document = strings.TrimSuffix(strings.TrimSpace(document), "}") + `, "routing": ` + routing + `}`
	}
	return document
}

const (
	directOutbound = `{"tag": "direct", "protocol": {"type": "freedom"}}`
	proxyOutbound  = `{"tag": "proxy", "server": "example.com", "server_port": 1080, "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}`
)

func groupOutbound(tag, groupType, settings string) string {
	return `{"tag": "` + tag + `", "protocol": {"type": "` + groupType + `", "settings": ` + settings + `}}`
}

func TestGroupsLoadWithTheirSettings(t *testing.T) {
	document := documentWithOutbounds(strings.Join([]string{
		groupOutbound("fastest", "urltest", `{"outbounds": ["direct", "proxy"], "tolerance": "80ms",
			"probe": {"type": "https", "url": "https://example.com/ok", "interval": "2m", "timeout": "3s"}}`),
		groupOutbound("first-working", "fallback", `{"outbounds": ["proxy", "direct"], "probe": {"type": "http"}}`),
		directOutbound, proxyOutbound,
	}, ","), "")

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("unexpected errors:\n%v", diagnostics.Err())
	}

	configuration, _ := config.Parse([]byte(document), modules(t))
	fastest := configuration.Outbounds[0].Group
	if fastest == nil || fastest.Type != blueprint.GroupTypeURLTest {
		t.Fatalf("outbounds[0].Group = %+v, want a urltest group", fastest)
	}
	if got, want := fastest.Members, []string{"direct", "proxy"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("members = %v, want %v", got, want)
	}
	wantProbe := blueprint.Probe{Type: blueprint.ProbeTypeHTTPS, URL: "https://example.com/ok", Interval: 2 * time.Minute, Timeout: 3 * time.Second}
	if fastest.Probe != wantProbe {
		t.Errorf("probe = %+v, want %+v", fastest.Probe, wantProbe)
	}
	if fastest.Tolerance != 80*time.Millisecond {
		t.Errorf("tolerance = %s, want 80ms", fastest.Tolerance)
	}
	if configuration.Outbounds[1].Group.Type != blueprint.GroupTypeFallback {
		t.Errorf("outbounds[1] is %q, want fallback", configuration.Outbounds[1].Group.Type)
	}
}

func TestGroupChecks(t *testing.T) {
	validProbe := `"probe": {"type": "http"}`
	for _, tc := range []struct {
		name      string
		outbounds string
		path      string
		message   string
	}{
		{"no probe", groupOutbound("g", "urltest", `{"outbounds": ["direct"]}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.type", "needs a probe"},
		{"unknown probe type", groupOutbound("g", "urltest", `{"outbounds": ["direct"], "probe": {"type": "icmp"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.type", `unknown probe type "icmp"`},
		{"https url for an http probe", groupOutbound("g", "fallback", `{"outbounds": ["direct"], "probe": {"type": "http", "url": "https://example.com/"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.url", "http://"},
		{"negative interval", groupOutbound("g", "fallback", `{"outbounds": ["direct"], "probe": {"type": "http", "interval": "-1s"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.interval", "must not be negative"},
		{"negative timeout", groupOutbound("g", "fallback", `{"outbounds": ["direct"], "probe": {"type": "http", "timeout": "-1s"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.timeout", "must not be negative"},
		{"no members", groupOutbound("g", "fallback", `{"outbounds": [], `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.outbounds", "needs members"},
		{"unknown member", groupOutbound("g", "fallback", `{"outbounds": ["nowhere"], `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.outbounds[0]", `no outbound is tagged "nowhere"`},
		{"its own member", groupOutbound("g", "fallback", `{"outbounds": ["g", "direct"], `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.outbounds[0]", "its own member"},
		{"tolerance on fallback", groupOutbound("g", "fallback", `{"outbounds": ["direct"], "tolerance": "10ms", `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.tolerance", "never compares speed"},
		{"negative tolerance", groupOutbound("g", "urltest", `{"outbounds": ["direct"], "tolerance": "-10ms", `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.tolerance", "must not be negative"},
		{"a server", `{"tag": "g", "server": "example.com", "server_port": 1, "protocol": {"type": "fallback", "settings": {"outbounds": ["direct"], ` + validProbe + `}}},` + directOutbound,
			"outbounds[0].server", "takes no server"},
		{"a dns_server", `{"tag": "g", "dns_server": "x", "protocol": {"type": "fallback", "settings": {"outbounds": ["direct"], ` + validProbe + `}}},` + directOutbound,
			"outbounds[0].dns_server", "set dns_server on a member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, documentWithOutbounds(tc.outbounds, ""))
			if !failed || !find(diagnostics, tc.path, tc.message) {
				t.Errorf("want an error at %s containing %q, got:\n%v", tc.path, tc.message, diagnostics)
			}
		})
	}
}

func TestGroupsThatPickAmongEachOtherAreRefused(t *testing.T) {
	validProbe := `"probe": {"type": "http"}`
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		groupOutbound("a", "fallback", `{"outbounds": ["b", "direct"], `+validProbe+`}`),
		groupOutbound("b", "fallback", `{"outbounds": ["a"], `+validProbe+`}`),
		directOutbound,
	}, ","), ""))
	if !failed || !find(diagnostics, "outbounds[0]", "these groups pick among each other") && !find(diagnostics, "outbounds[1]", "these groups pick among each other") {
		t.Errorf("a loop between groups was not reported:\n%v", diagnostics)
	}
}

func TestAGroupMayBeNestedAndListedBeforeItsMembers(t *testing.T) {
	validProbe := `"probe": {"type": "http"}`
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		groupOutbound("outer", "fallback", `{"outbounds": ["inner", "direct"], `+validProbe+`}`),
		groupOutbound("inner", "urltest", `{"outbounds": ["proxy"], `+validProbe+`}`),
		directOutbound, proxyOutbound,
	}, ","), ""))
	if failed {
		t.Errorf("unexpected errors:\n%v", diagnostics.Err())
	}
}

func TestAGroupOfOneMemberIsWarnedAbout(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(
		groupOutbound("g", "fallback", `{"outbounds": ["direct"], "probe": {"type": "http"}}`)+","+directOutbound, ""))
	if failed || !find(diagnostics, "outbounds[0].protocol.settings.outbounds", "nothing to pick between") {
		t.Errorf("want only a warning about one member, got:\n%v", diagnostics)
	}
}

func TestAGroupCarriesUDPOnlyWhenEveryMemberDoes(t *testing.T) {
	validProbe := `"probe": {"type": "http"}`
	routing := `{"rules": [{"match": {"network": "udp"}, "action": {"type": "route", "settings": {"outbound": "g"}}}],
		"default_action": {"type": "route", "settings": {"outbound": "direct"}}}`
	// socks5 frames UDP in its own streams only through a datagram-over-stream
	// kind, which a SOCKS5 outbound does not have.
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		directOutbound, proxyOutbound,
		groupOutbound("g", "fallback", `{"outbounds": ["direct", "proxy"], `+validProbe+`}`),
	}, ","), routing))
	if failed || !find(diagnostics, "routing.rules[0].action.settings.outbound", "cannot carry UDP") {
		t.Errorf("want a UDP warning for a group with a TCP-only member, got:\n%v", diagnostics)
	}

	diagnostics, failed = loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		directOutbound,
		`{"tag": "direct-two", "protocol": {"type": "freedom"}}`,
		groupOutbound("g", "fallback", `{"outbounds": ["direct", "direct-two"], `+validProbe+`}`),
	}, ","), routing))
	if failed || find(diagnostics, "routing.rules[0].action.settings.outbound", "cannot carry UDP") {
		t.Errorf("a group of freedoms carries UDP, got:\n%v", diagnostics)
	}
}

func TestAGroupTakesNoTransport(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(
		`{"tag": "g", "transport": [{"type": "tcp"}], "protocol": {"type": "fallback", "settings": {"outbounds": ["direct"], "probe": {"type": "http"}}}},`+directOutbound, ""))
	if !failed || !find(diagnostics, "outbounds[0].transport", "has no transport of its own") {
		t.Errorf("want an error at outbounds[0].transport, got:\n%v", diagnostics)
	}
}

// A dns server that goes through a group goes through each member, so a member
// that needs the same server for its own name is a loop.
func TestDNSLoopThroughAGroupIsRefusedAndNamed(t *testing.T) {
	dns := `{"servers": [{"tag": "google", "type": "doh", "settings": {"server": "8.8.8.8"}, "tls": {"alpn": ["http/1.1"]}, "outbound": "auto"}], "default_server": "google"}`
	group := groupOutbound("auto", "fallback", `{"outbounds": ["proxy", "direct"], "probe": {"type": "http"}}`)
	document := strings.Replace(withDNS(dns, ""), `"outbounds": [`, `"outbounds": [`+group+`,`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	want := `dns server "google" → outbound "auto" → outbound "proxy" → dns server "google"`
	if !failed || !find(diagnostics, "dns.servers[0]", want) {
		t.Errorf("want the loop %s reported; got:\n%v", want, diagnostics)
	}
}
