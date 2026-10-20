package proxies_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/we-apranik/apranik/module/api/clash/internal/testkit"
	"github.com/we-apranik/apranik/spec"
)

type measuredWith struct {
	mu                    sync.Mutex
	tag, rawURL, expected string
	timeout               time.Duration
	calls                 int
}

func (m *measuredWith) function(delay time.Duration, err error) func(context.Context, string, string, time.Duration, string) (time.Duration, error) {
	return func(_ context.Context, tag, rawURL string, timeout time.Duration, expected string) (time.Duration, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.tag, m.rawURL, m.timeout, m.expected = tag, rawURL, timeout, expected
		m.calls++
		return delay, err
	}
}

func TestDelayUsesTheQueryAndThenTheDefaultsItWasGiven(t *testing.T) {
	var measured measuredWith
	provider := outboundFixtures()
	provider.MeasureFunc = measured.function(42*time.Millisecond, nil)
	server := mount(t, provider)

	var reply struct{ Delay int64 }
	if status := server.GetJSON("/proxies/server-a/delay", &reply); status != http.StatusOK || reply.Delay != 42 {
		t.Fatalf("delay = %d %+v", status, reply)
	}
	if measured.tag != "server-a" || measured.rawURL != "https://default.example/204" || measured.timeout != 10*time.Second || measured.expected != "200-299" {
		t.Errorf("asked %+v, want the defaults", &measured)
	}

	server.GetJSON("/proxies/direct/delay?url="+url.QueryEscape("http://q.example/x")+"&timeout=1500&expected=301", &reply)
	if measured.rawURL != "http://q.example/x" || measured.timeout != 1500*time.Millisecond || measured.expected != "301" {
		t.Errorf("asked %+v, want the query's values", &measured)
	}
	server.GetJSON("/proxies/direct/delay?expected_status=200,204", &reply)
	if measured.expected != "200,204" || measured.rawURL != "https://default.example/204" {
		t.Errorf("asked %+v, want expected_status accepted as the settings' own name and the rest defaulted", &measured)
	}
}

func TestADelayOfUnderAMillisecondIsOneBecauseZeroMeansFailedToADashboard(t *testing.T) {
	var measured measuredWith
	provider := outboundFixtures()
	provider.MeasureFunc = measured.function(300*time.Microsecond, nil)
	var reply struct{ Delay int64 }
	mount(t, provider).GetJSON("/proxies/direct/delay", &reply)
	if reply.Delay != 1 {
		t.Errorf("delay = %d, want 1", reply.Delay)
	}
}

func TestDelayErrorsHaveTheStatusDashboardsExpect(t *testing.T) {
	for name, test := range map[string]struct {
		err    error
		status int
	}{
		"no such outbound": {spec.ErrAPINoSuchOutbound, http.StatusNotFound},
		"invalid request":  {spec.ErrAPIInvalidDelayTest, http.StatusBadRequest},
		"timeout":          {context.DeadlineExceeded, http.StatusGatewayTimeout},
		"wrapped timeout":  {errors.Join(errors.New("dial"), context.DeadlineExceeded), http.StatusGatewayTimeout},
		"a failure":        {errors.New("connection refused"), http.StatusServiceUnavailable},
	} {
		var measured measuredWith
		provider := outboundFixtures()
		provider.MeasureFunc = measured.function(0, test.err)
		status, body, _ := mount(t, provider).Do("GET", "/proxies/direct/delay", "")
		if status != test.status || testkit.MessageOf(t, body) == "" {
			t.Errorf("%s: %d %q, want %d", name, status, body, test.status)
		}
	}
}

func TestDelayRefusesATimeoutThatIsNotAPositiveWholeNumber(t *testing.T) {
	var measured measuredWith
	provider := outboundFixtures()
	provider.MeasureFunc = measured.function(time.Millisecond, nil)
	server := mount(t, provider)
	for _, timeout := range []string{"abc", "0", "-1", "1.5s"} {
		if status, _, _ := server.Do("GET", "/proxies/direct/delay?timeout="+timeout, ""); status != http.StatusBadRequest {
			t.Errorf("timeout %q: status %d, want 400", timeout, status)
		}
	}
	if measured.calls != 0 {
		t.Errorf("%d measurements were made for bad requests", measured.calls)
	}
}

func TestGroupDelayMeasuresEveryMemberAndLeavesOutTheOnesThatFailed(t *testing.T) {
	provider := outboundFixtures()
	provider.MeasureFunc = func(_ context.Context, tag, _ string, _ time.Duration, _ string) (time.Duration, error) {
		if tag == "direct" {
			return 0, errors.New("unreachable")
		}
		return 80 * time.Millisecond, nil
	}
	var reply map[string]int64
	if status := mount(t, provider).GetJSON("/group/choose/delay", &reply); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if len(reply) != 1 || reply["server-a"] != 80 {
		t.Errorf("reply = %v, want only server-a at 80", reply)
	}
}

func TestGroupDelayRunsMembersAtTheSameTimeButNotAllAtOnce(t *testing.T) {
	members := make([]string, 20)
	states := map[string]spec.APIOutboundState{}
	for index := range members {
		members[index] = "m" + string(rune('a'+index))
		states[members[index]] = spec.APIOutboundState{Tag: members[index], Kind: spec.APIOutboundKindFreedom}
	}
	states["big"] = spec.APIOutboundState{Tag: "big", Kind: spec.APIOutboundKindURLTest, Group: &spec.APIGroupState{MemberTags: members}}

	var mu sync.Mutex
	running, highest := 0, 0
	provider := &testkit.Provider{Tags: []string{"big"}, States: states}
	provider.MeasureFunc = func(context.Context, string, string, time.Duration, string) (time.Duration, error) {
		mu.Lock()
		running++
		highest = max(highest, running)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return time.Millisecond, nil
	}
	var reply map[string]int64
	mount(t, provider).GetJSON("/group/big/delay", &reply)
	if len(reply) != 20 || highest < 2 || highest > 8 {
		t.Errorf("%d results with at most %d at once, want 20 results and between 2 and 8 at once", len(reply), highest)
	}
}

func TestGroupDelayNeedsAGroupAndGoodParameters(t *testing.T) {
	var measured measuredWith
	provider := outboundFixtures()
	provider.MeasureFunc = measured.function(time.Millisecond, nil)
	server := mount(t, provider)

	if status, body, _ := server.Do("GET", "/group/server-a/delay", ""); status != http.StatusNotFound || !strings.Contains(testkit.MessageOf(t, body), "not a group") {
		t.Errorf("a proxy: %d %q", status, body)
	}
	if status, _, _ := server.Do("GET", "/group/missing/delay", ""); status != http.StatusNotFound {
		t.Errorf("missing: %d", status)
	}
	if status, _, _ := server.Do("GET", "/group/choose/delay?timeout=zero", ""); status != http.StatusBadRequest {
		t.Errorf("bad timeout: %d", status)
	}

	provider.MeasureFunc = func(context.Context, string, string, time.Duration, string) (time.Duration, error) {
		return 0, spec.ErrAPIInvalidDelayTest
	}
	if status, _, _ := server.Do("GET", "/group/choose/delay?expected=bogus", ""); status != http.StatusBadRequest {
		t.Errorf("an expected status core refuses: %d, want 400", status)
	}
}
