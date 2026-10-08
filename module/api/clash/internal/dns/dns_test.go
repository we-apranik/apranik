package dns_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/dns"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, _ *stream.Hub) { dns.Register(router, provider) })
}

func TestDNSQueryAnswersInMihomosShape(t *testing.T) {
	var gotName, gotType string
	provider := &testkit.Provider{QueryDNSFunc: func(_ context.Context, name, recordType string) (spec.APIDNSAnswer, error) {
		gotName, gotType = name, recordType
		return spec.APIDNSAnswer{ServerTag: "cloudflare", Records: []spec.APIDNSRecord{
			{Name: "example.com.", Type: "A", TTL: 90 * time.Second, Data: "192.0.2.1"},
			{Name: "example.com.", Type: "A", TTL: 90 * time.Second, Data: "192.0.2.2"},
		}}, nil
	}}
	server := mount(t, provider)

	var reply struct {
		Status   int `json:"Status"`
		Question []struct {
			Name   string `json:"Name"`
			Qtype  int    `json:"Qtype"`
			Qclass int    `json:"Qclass"`
		} `json:"Question"`
		Server string `json:"Server"`
		Answer []struct {
			Name string `json:"name"`
			Type int    `json:"type"`
			TTL  uint32 `json:"TTL"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if status := server.GetJSON("/dns/query?name=example.com&type=a", &reply); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if gotName != "example.com" || gotType != "A" {
		t.Errorf("asked core for %q %q", gotName, gotType)
	}
	if reply.Status != 0 || len(reply.Question) != 1 || reply.Question[0].Name != "example.com." || reply.Question[0].Qtype != 1 || reply.Question[0].Qclass != 1 ||
		reply.Server != "cloudflare" || len(reply.Answer) != 2 || reply.Answer[1].Data != "192.0.2.2" || reply.Answer[0].TTL != 90 || reply.Answer[0].Type != 1 {
		t.Errorf("reply = %+v", reply)
	}
}

func TestDNSQueryDefaultsToTypeAAndNamesAAAAByItsNumber(t *testing.T) {
	var gotType string
	provider := &testkit.Provider{QueryDNSFunc: func(_ context.Context, _, recordType string) (spec.APIDNSAnswer, error) {
		gotType = recordType
		return spec.APIDNSAnswer{Records: []spec.APIDNSRecord{{Name: "x.", Type: "AAAA", Data: "2001:db8::1"}}}, nil
	}}
	server := mount(t, provider)
	server.Do("GET", "/dns/query?name=x.example", "")
	if gotType != "A" {
		t.Errorf("default type = %q, want A", gotType)
	}
	_, body, _ := server.Do("GET", "/dns/query?name=x.example&type=AAAA", "")
	if !strings.Contains(body, `"type":28`) {
		t.Errorf("body = %s, want AAAA as 28", body)
	}
}

func TestDNSQueryWithNoRecordsHasNoAnswerKey(t *testing.T) {
	provider := &testkit.Provider{QueryDNSFunc: func(context.Context, string, string) (spec.APIDNSAnswer, error) { return spec.APIDNSAnswer{}, nil }}
	_, body, _ := mount(t, provider).Do("GET", "/dns/query?name=none.example", "")
	if strings.Contains(body, "Answer") || !strings.Contains(body, `"Status":0`) {
		t.Errorf("body = %s", body)
	}
}

func TestDNSQueryErrors(t *testing.T) {
	for name, test := range map[string]struct {
		err    error
		status int
	}{
		"invalid":     {spec.ErrAPIInvalidDNSQuery, http.StatusBadRequest},
		"wrapped":     {errors.Join(errors.New("detail"), spec.ErrAPIInvalidDNSQuery), http.StatusBadRequest},
		"lookup fail": {errors.New("no server answered"), http.StatusServiceUnavailable},
	} {
		provider := &testkit.Provider{QueryDNSFunc: func(context.Context, string, string) (spec.APIDNSAnswer, error) { return spec.APIDNSAnswer{}, test.err }}
		status, body, _ := mount(t, provider).Do("GET", "/dns/query?name=x.example", "")
		if status != test.status || testkit.MessageOf(t, body) == "" {
			t.Errorf("%s: %d %q, want %d", name, status, body, test.status)
		}
	}
}

func TestFlushingTheDNSCache(t *testing.T) {
	flushed := 0
	server := mount(t, &testkit.Provider{FlushDNSFunc: func() error { flushed++; return nil }})
	if status, body, _ := server.Do("POST", "/cache/dns/flush", ""); status != http.StatusNoContent || body != "" || flushed != 1 {
		t.Errorf("flush = %d %q (%d flushes)", status, body, flushed)
	}
	if status, _, _ := server.Do("GET", "/cache/dns/flush", ""); status != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405: flushing is a POST", status)
	}
	failing := mount(t, &testkit.Provider{FlushDNSFunc: func() error { return errors.New("no resolver") }})
	if status, _, _ := failing.Do("POST", "/cache/dns/flush", ""); status != http.StatusInternalServerError {
		t.Errorf("a failed flush = %d, want 500", status)
	}
}
