package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config"
	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

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
		groupOutbound("fastest", "urltest", `{"members": ["direct", "proxy"], "tolerance": "80ms",
			"probe": {"url": "https://example.com/ok", "interval": "2m", "timeout": "3s", "expected_status": "200,204"}}`),
		groupOutbound("first-working", "fallback", `{"members": ["proxy", "direct"]}`),
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
	wantProbe := blueprint.Probe{
		URL: "https://example.com/ok", Interval: types.Pointer(2 * time.Minute), Timeout: types.Pointer(3 * time.Second),
		ExpectedStatuses: []httpstatusranges.Range{{First: 200, Last: 200}, {First: 204, Last: 204}},
	}
	if !reflect.DeepEqual(fastest.Probe, wantProbe) {
		t.Errorf("probe = %+v, want %+v", fastest.Probe, wantProbe)
	}
	if fallback := configuration.Outbounds[1].Group; !reflect.DeepEqual(fallback.Probe, blueprint.Probe{}) {
		t.Errorf("a probe left out is %+v, want every field unset so the group's defaults apply", fallback.Probe)
	}
	if fastest.Tolerance == nil || *fastest.Tolerance != 80*time.Millisecond {
		t.Errorf("tolerance = %v, want 80ms", fastest.Tolerance)
	}
	if configuration.Outbounds[1].Group.Type != blueprint.GroupTypeFallback {
		t.Errorf("outbounds[1] is %q, want fallback", configuration.Outbounds[1].Group.Type)
	}
}

func TestGroupChecks(t *testing.T) {
	validProbe := `"probe": {"url": "http://example.com/"}`
	for _, tc := range []struct {
		name      string
		outbounds string
		path      string
		message   string
	}{
		{"a probe url that is not http or https", groupOutbound("g", "urltest", `{"members": ["direct"], "probe": {"url": "ftp://example.com/"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.url", "http:// or https://"},
		{"a probe url with no host", groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"url": "http:///path"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.url", "names no host"},
		{"negative interval", groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"interval": "-1s"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.interval", "greater than zero"},
		{"zero interval", groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"interval": "0s"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.interval", "greater than zero"},
		{"negative timeout", groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"timeout": "-1s"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.probe.timeout", "greater than zero"},
		{"a bad expected_status", groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"expected_status": "700"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings", "100 to 599"},
		{"a bare number for expected_status", groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"expected_status": 204}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings", `quoted string such as "204"`},
		{"the old probe type", groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"type": "http"}}`) + "," + directOutbound,
			"outbounds[0].protocol.settings", `unknown field "type"`},
		{"the old outbounds key", groupOutbound("g", "fallback", `{"outbounds": ["direct"]}`) + "," + directOutbound,
			"outbounds[0].protocol.settings", `unknown field "outbounds"`},
		{"no members", groupOutbound("g", "fallback", `{"members": [], `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.members", "needs members"},
		{"unknown member", groupOutbound("g", "fallback", `{"members": ["nowhere"], `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.members[0]", `no outbound is tagged "nowhere"`},
		{"its own member", groupOutbound("g", "fallback", `{"members": ["g", "direct"], `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.members[0]", "its own member"},
		{"tolerance on fallback", groupOutbound("g", "fallback", `{"members": ["direct"], "tolerance": "10ms", `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.tolerance", "never compares speed"},
		{"negative tolerance", groupOutbound("g", "urltest", `{"members": ["direct"], "tolerance": "-10ms", `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.tolerance", "greater than zero"},
		{"zero tolerance", groupOutbound("g", "urltest", `{"members": ["direct"], "tolerance": "0s", `+validProbe+`}`) + "," + directOutbound,
			"outbounds[0].protocol.settings.tolerance", "greater than zero"},
		{"a server", `{"tag": "g", "server": "example.com", "server_port": 1, "protocol": {"type": "fallback", "settings": {"members": ["direct"], ` + validProbe + `}}},` + directOutbound,
			"outbounds[0].server", "takes no server"},
		{"a dns_server", `{"tag": "g", "dns_server": "x", "protocol": {"type": "fallback", "settings": {"members": ["direct"], ` + validProbe + `}}},` + directOutbound,
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
	validProbe := `"probe": {"url": "http://example.com/"}`
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		groupOutbound("a", "fallback", `{"members": ["b", "direct"], `+validProbe+`}`),
		groupOutbound("b", "fallback", `{"members": ["a"], `+validProbe+`}`),
		directOutbound,
	}, ","), ""))
	if !failed || !find(diagnostics, "outbounds[0]", "these groups pick among each other") && !find(diagnostics, "outbounds[1]", "these groups pick among each other") {
		t.Errorf("a loop between groups was not reported:\n%v", diagnostics)
	}
}

func TestAGroupMayBeNestedAndListedBeforeItsMembers(t *testing.T) {
	validProbe := `"probe": {"url": "http://example.com/"}`
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		groupOutbound("outer", "fallback", `{"members": ["inner", "direct"], `+validProbe+`}`),
		groupOutbound("inner", "urltest", `{"members": ["proxy"], `+validProbe+`}`),
		directOutbound, proxyOutbound,
	}, ","), ""))
	if failed {
		t.Errorf("unexpected errors:\n%v", diagnostics.Err())
	}
}

func TestAGroupOfOneMemberIsWarnedAbout(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(
		groupOutbound("g", "fallback", `{"members": ["direct"], "probe": {"url": "http://example.com/"}}`)+","+directOutbound, ""))
	if failed || !find(diagnostics, "outbounds[0].protocol.settings.members", "nothing to pick between") {
		t.Errorf("want only a warning about one member, got:\n%v", diagnostics)
	}
}

func TestAGroupCarriesUDPOnlyWhenEveryMemberDoes(t *testing.T) {
	validProbe := `"probe": {"url": "http://example.com/"}`
	routing := `{"rules": [{"match": {"network": "udp"}, "action": {"type": "route", "settings": {"outbound": "g"}}}],
		"default_action": {"type": "route", "settings": {"outbound": "direct"}}}`
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		directOutbound, proxyOutbound,
		groupOutbound("g", "fallback", `{"members": ["direct", "proxy"], `+validProbe+`}`),
	}, ","), routing))
	if failed || !find(diagnostics, "routing.rules[0].action.settings.outbound", "cannot carry UDP") {
		t.Errorf("want a UDP warning for a group with a TCP-only member, got:\n%v", diagnostics)
	}

	diagnostics, failed = loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		directOutbound,
		`{"tag": "direct-two", "protocol": {"type": "freedom"}}`,
		groupOutbound("g", "fallback", `{"members": ["direct", "direct-two"], `+validProbe+`}`),
	}, ","), routing))
	if failed || find(diagnostics, "routing.rules[0].action.settings.outbound", "cannot carry UDP") {
		t.Errorf("a group of freedoms carries UDP, got:\n%v", diagnostics)
	}
}

func TestAGroupTakesNoTransport(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(
		`{"tag": "g", "transport": [{"type": "tcp"}], "protocol": {"type": "fallback", "settings": {"members": ["direct"], "probe": {"url": "http://example.com/"}}}},`+directOutbound, ""))
	if !failed || !find(diagnostics, "outbounds[0].transport", "has no transport of its own") {
		t.Errorf("want an error at outbounds[0].transport, got:\n%v", diagnostics)
	}
}

func TestDNSLoopThroughAGroupIsRefusedAndNamed(t *testing.T) {
	dns := `{"servers": [{"tag": "google", "type": "doh", "settings": {"server": "8.8.8.8"}, "tls": {"alpn": ["http/1.1"]}, "outbound": "auto"}], "default_server": "google"}`
	group := groupOutbound("auto", "fallback", `{"members": ["proxy", "direct"], "probe": {"url": "http://example.com/"}}`)
	document := strings.Replace(withDNS(dns, ""), `"outbounds": [`, `"outbounds": [`+group+`,`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	want := `dns server "google" → outbound "auto" → outbound "proxy" → dns server "google"`
	if !failed || !find(diagnostics, "dns.servers[0]", want) {
		t.Errorf("want the loop %s reported; got:\n%v", want, diagnostics)
	}
}

func TestASelectorLoadsWithMembersOnly(t *testing.T) {
	document := documentWithOutbounds(strings.Join([]string{
		groupOutbound("proxy", "selector", `{"members": ["direct", "other"]}`),
		groupOutbound("other", "fallback", `{"members": ["direct"]}`),
		directOutbound,
	}, ","), "")

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("unexpected errors:\n%v", diagnostics.Err())
	}
	configuration, _ := config.Parse([]byte(document), modules(t))
	selector := configuration.Outbounds[0].Group
	if selector == nil || selector.Type != blueprint.GroupTypeSelector || len(selector.Members) != 2 {
		t.Fatalf("outbounds[0].Group = %+v, want a selector with two members", selector)
	}
}

func TestASelectorRefusesWhatOnlyAnAutomaticGroupUses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings string
		path     string
		message  string
	}{
		{"a probe", `{"members": ["direct"], "probe": {"url": "http://example.com/"}}`, "outbounds[0].protocol.settings.probe", "takes no probe"},
		{"an empty probe", `{"members": ["direct"], "probe": {}}`, "outbounds[0].protocol.settings.probe", "takes no probe"},
		{"a tolerance", `{"members": ["direct"], "tolerance": "10ms"}`, "outbounds[0].protocol.settings.tolerance", "never compares speed"},
		{"no members", `{"members": []}`, "outbounds[0].protocol.settings.members", "needs members"},
		{"a member that is not there", `{"members": ["nowhere"]}`, "outbounds[0].protocol.settings.members[0]", `no outbound is tagged "nowhere"`},
		{"itself", `{"members": ["proxy", "direct"]}`, "outbounds[0].protocol.settings.members[0]", "its own member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, documentWithOutbounds(groupOutbound("proxy", "selector", tc.settings)+","+directOutbound, ""))
			if !failed || !find(diagnostics, tc.path, tc.message) {
				t.Errorf("want an error at %s containing %q, got:\n%v", tc.path, tc.message, diagnostics)
			}
		})
	}
}

func TestSelectorsThatChooseEachOtherAreRefusedButOneMayChooseAnother(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		groupOutbound("a", "selector", `{"members": ["b", "direct"]}`),
		groupOutbound("b", "selector", `{"members": ["a"]}`),
		directOutbound,
	}, ","), ""))
	if !failed || !find(diagnostics, "outbounds[0]", "these groups pick among each other") && !find(diagnostics, "outbounds[1]", "these groups pick among each other") {
		t.Errorf("a loop between selectors was not reported:\n%v", diagnostics)
	}

	diagnostics, failed = loadAndCheck(t, documentWithOutbounds(strings.Join([]string{
		groupOutbound("outer", "selector", `{"members": ["inner", "direct"]}`),
		groupOutbound("inner", "selector", `{"members": ["direct", "proxy"]}`),
		directOutbound, proxyOutbound,
	}, ","), ""))
	if failed {
		t.Errorf("a selector inside a selector was refused:\n%v", diagnostics.Err())
	}
}
