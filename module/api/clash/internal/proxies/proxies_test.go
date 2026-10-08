package proxies_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/proxies"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, _ *stream.Hub) {
		proxies.Register(router, provider, proxies.DelayDefaults{URL: "https://default.example/204", Timeout: 10 * time.Second, ExpectedStatus: "200-299"})
	})
}

func outboundFixtures() *testkit.Provider {
	return &testkit.Provider{
		Tags: []string{"server-a", "direct", "choose", "auto", "fail"},
		States: map[string]spec.APIOutboundState{
			"server-a": {Tag: "server-a", Kind: spec.APIOutboundKindProxy, ProtocolName: "trojan", CarriesUDP: true},
			"direct":   {Tag: "direct", Kind: spec.APIOutboundKindFreedom, CarriesUDP: true},
			"choose":   {Tag: "choose", Kind: spec.APIOutboundKindSelector, Group: &spec.APIGroupState{MemberTags: []string{"server-a", "direct"}, CurrentMember: "server-a"}},
			"auto":     {Tag: "auto", Kind: spec.APIOutboundKindURLTest, Group: &spec.APIGroupState{MemberTags: []string{"server-a", "direct"}, CurrentMember: "direct"}},
			"fail":     {Tag: "fail", Kind: spec.APIOutboundKindFallback, Group: &spec.APIGroupState{MemberTags: []string{"direct"}, CurrentMember: "direct"}},
		},
		DefaultTag: "choose",
	}
}

type proxyReply struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	UDP     bool            `json:"udp"`
	History json.RawMessage `json:"history"`
	Now     *string         `json:"now"`
	All     []string        `json:"all"`
}

func TestProxiesListsEveryOutboundWithClashTypesAndTheGlobalGroup(t *testing.T) {
	server := mount(t, outboundFixtures())

	var reply struct {
		Proxies map[string]proxyReply `json:"proxies"`
	}
	if status := server.GetJSON("/proxies", &reply); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	wantTypes := map[string]string{"server-a": "Trojan", "direct": "Direct", "choose": "Selector", "auto": "URLTest", "fail": "Fallback", "GLOBAL": "Fallback"}
	for name, wantType := range wantTypes {
		if got := reply.Proxies[name]; got.Type != wantType || got.Name != name || string(got.History) != "[]" {
			t.Errorf("%s = %+v, want type %s with an empty history", name, got, wantType)
		}
	}
	if len(reply.Proxies) != len(wantTypes) {
		t.Errorf("%d proxies, want %d", len(reply.Proxies), len(wantTypes))
	}

	if server := reply.Proxies["server-a"]; !server.UDP || server.Now != nil || server.All != nil {
		t.Errorf("server-a = %+v: a proxy has udp and no now or all", server)
	}
	if choose := reply.Proxies["choose"]; choose.Now == nil || *choose.Now != "server-a" || len(choose.All) != 2 || choose.All[0] != "server-a" {
		t.Errorf("choose = %+v", choose)
	}
	if global := reply.Proxies["GLOBAL"]; global.Now == nil || *global.Now != "choose" || len(global.All) != 5 {
		t.Errorf("GLOBAL = %+v, want the default outbound as now and every outbound in all", global)
	}
}

func TestGlobalFallsBackToTheFirstOutboundWhenTheDefaultActionBlocks(t *testing.T) {
	provider := outboundFixtures()
	provider.DefaultTag = ""
	var reply struct {
		Proxies map[string]proxyReply `json:"proxies"`
	}
	mount(t, provider).GetJSON("/proxies", &reply)
	if now := reply.Proxies["GLOBAL"].Now; now == nil || *now != "server-a" {
		t.Errorf("GLOBAL now = %v, want the first outbound", now)
	}
}

func TestARealOutboundTaggedGlobalIsNotReplacedByTheView(t *testing.T) {
	provider := &testkit.Provider{
		Tags:   []string{"GLOBAL"},
		States: map[string]spec.APIOutboundState{"GLOBAL": {Tag: "GLOBAL", Kind: spec.APIOutboundKindFreedom}},
	}
	var reply struct {
		Proxies map[string]proxyReply `json:"proxies"`
	}
	mount(t, provider).GetJSON("/proxies", &reply)
	if reply.Proxies["GLOBAL"].Type != "Direct" {
		t.Errorf("GLOBAL = %+v, want the real outbound", reply.Proxies["GLOBAL"])
	}
}

func TestOneProxyByNameIncludingGlobalAndEncodedNames(t *testing.T) {
	provider := outboundFixtures()
	provider.Tags = append(provider.Tags, "a b/c")
	provider.States["a b/c"] = spec.APIOutboundState{Tag: "a b/c", Kind: spec.APIOutboundKindFreedom}
	server := mount(t, provider)

	var one proxyReply
	if status := server.GetJSON("/proxies/auto", &one); status != http.StatusOK || one.Type != "URLTest" || one.Now == nil || *one.Now != "direct" {
		t.Errorf("auto = %d %+v", status, one)
	}
	if status := server.GetJSON("/proxies/GLOBAL", &one); status != http.StatusOK || one.Name != "GLOBAL" || len(one.All) != 6 {
		t.Errorf("GLOBAL = %d %+v", status, one)
	}
	if status := server.GetJSON("/proxies/"+url.PathEscape("a b/c"), &one); status != http.StatusOK || one.Name != "a b/c" {
		t.Errorf("an encoded name = %d %+v", status, one)
	}
	if status, _, _ := server.Do("GET", "/proxies/missing", ""); status != http.StatusNotFound {
		t.Errorf("missing = %d, want 404", status)
	}
}
