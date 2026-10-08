package proxies_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/spec"
)

func TestSwitchingASelector(t *testing.T) {
	var selectorTag, memberTag string
	provider := outboundFixtures()
	provider.SelectMemberFunc = func(selector, member string) error {
		if member == "ghost" {
			return spec.ErrAPINoSuchMember
		}
		selectorTag, memberTag = selector, member
		return nil
	}
	server := mount(t, provider)

	if status, body, _ := server.Do("PUT", "/proxies/choose", `{"name":"direct"}`); status != http.StatusNoContent || body != "" || selectorTag != "choose" || memberTag != "direct" {
		t.Errorf("switch = %d %q (%s -> %s)", status, body, selectorTag, memberTag)
	}

	status, body, _ := server.Do("PUT", "/proxies/choose", `{"name":"ghost"}`)
	if message := testkit.MessageOf(t, body); status != http.StatusBadRequest || !strings.Contains(message, "ghost") || !strings.Contains(message, "server-a, direct") {
		t.Errorf("unknown member = %d %q, want 400 listing the members", status, body)
	}
}

func TestSwitchingAnythingButASelectorIsRefusedAndSaysWhy(t *testing.T) {
	server := mount(t, outboundFixtures())
	for name, want := range map[string]string{
		"auto":     "chooses its member by itself",
		"fail":     "chooses its member by itself",
		"server-a": "not a group",
		"GLOBAL":   "read-only view",
	} {
		status, body, _ := server.Do("PUT", "/proxies/"+name, `{"name":"direct"}`)
		if status != http.StatusBadRequest || !strings.Contains(testkit.MessageOf(t, body), want) {
			t.Errorf("PUT %s = %d %q, want 400 saying %q", name, status, body, want)
		}
	}
	if status, _, _ := server.Do("PUT", "/proxies/missing", `{"name":"direct"}`); status != http.StatusNotFound {
		t.Errorf("missing selector = %d, want 404", status)
	}
}

func TestSwitchingNeedsTheMemberNameInTheBody(t *testing.T) {
	server := mount(t, outboundFixtures())
	for _, body := range []string{``, `not json`, `{}`, `{"name":""}`, `{"name":"direct","extra":1}`, `{"name":5}`} {
		status, reply, _ := server.Do("PUT", "/proxies/choose", body)
		if status != http.StatusBadRequest || !strings.Contains(testkit.MessageOf(t, reply), `{"name"`) {
			t.Errorf("body %q = %d %q, want 400 showing the shape", body, status, reply)
		}
	}
}
