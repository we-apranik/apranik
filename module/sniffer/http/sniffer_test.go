package http_test

import (
	"errors"
	"testing"

	"github.com/we-apranik/apranik/module/sniffer/http"
	"github.com/we-apranik/apranik/spec"
)

func newSniffer(t *testing.T) spec.Sniffer {
	t.Helper()
	sniffer, err := http.NewSnifferModule().NewSniffer()
	if err != nil {
		t.Fatalf("NewSniffer: %v", err)
	}
	return sniffer
}

func TestTheHostAndPortAreReportedUnchanged(t *testing.T) {
	request := "GET /index.html HTTP/1.1\r\nUser-Agent: test\r\nhost:   Example.com:8080  \r\n\r\n"

	result, err := newSniffer(t).Sniff([]byte(request))
	if err != nil {
		t.Fatalf("Sniff: %v", err)
	}
	if result.Protocol != "http" || result.Domain != "Example.com" || result.Port != 8080 {
		t.Errorf("got %+v, want http Example.com 8080", result)
	}
}

func TestAnIncompleteHeaderAsksForMoreData(t *testing.T) {
	for _, prefix := range []string{"", "GE", "POST / HTTP/1.1\r\nHost: example"} {
		if _, err := newSniffer(t).Sniff([]byte(prefix)); !errors.Is(err, spec.ErrSniffNeedMoreData) {
			t.Errorf("%q returned %v, want ErrSniffNeedMoreData", prefix, err)
		}
	}
}

func TestOtherProtocolsAreRefusedAtOnce(t *testing.T) {
	for _, payloadPrefix := range []string{"\x16\x03\x01", "SSH-2.0-OpenSSH_9.6\r\n", "GETX / HTTP/1.1\r\n\r\n"} {
		if _, err := newSniffer(t).Sniff([]byte(payloadPrefix)); err == nil || errors.Is(err, spec.ErrSniffNeedMoreData) {
			t.Errorf("%q returned %v, want a refusal", payloadPrefix, err)
		}
	}
}

func TestAHostWithoutAPortReportsPortZero(t *testing.T) {
	for request, wantHost := range map[string]string{
		"GET / HTTP/1.1\r\nHost: example.com\r\n\r\n":   "example.com",
		"GET / HTTP/1.1\r\nHost: [2001:db8::1]\r\n\r\n": "2001:db8::1",
	} {
		result, err := newSniffer(t).Sniff([]byte(request))
		if err != nil || result.Domain != wantHost || result.Port != 0 {
			t.Errorf("%q: got %+v, %v; want %s and port 0", request, result, err, wantHost)
		}
	}
}

func TestRequestHeadersAreReportedWithMethodAndPath(t *testing.T) {
	request := "POST /upload?x=1 HTTP/1.1\r\nHost: example.com\r\nUser-Agent: curl/8.9.1\r\nAccept: text/html\r\nACCEPT: */*\r\n\r\n"

	result, err := newSniffer(t).Sniff([]byte(request))
	if err != nil {
		t.Fatalf("Sniff: %v", err)
	}
	want := map[string]string{
		":method":    "POST",
		":path":      "/upload?x=1",
		"host":       "example.com",
		"user-agent": "curl/8.9.1",
		"accept":     "text/html, */*",
	}
	if len(result.RequestHeaders) != len(want) {
		t.Errorf("headers = %v, want %v", result.RequestHeaders, want)
	}
	for name, wantValue := range want {
		if result.RequestHeaders[name] != wantValue {
			t.Errorf("header %q = %q, want %q", name, result.RequestHeaders[name], wantValue)
		}
	}
}

func TestOnlyTheFirstHostHeaderNamesTheDomain(t *testing.T) {
	result, err := newSniffer(t).Sniff([]byte("GET / HTTP/1.1\r\nHost: first.example\r\nHost: second.example\r\n\r\n"))
	if err != nil {
		t.Fatalf("Sniff: %v", err)
	}
	if result.Domain != "first.example" {
		t.Errorf("Domain = %q, want first.example", result.Domain)
	}
}

func BenchmarkSniffBrowserRequest(b *testing.B) {
	request := []byte("GET /search?q=apranik HTTP/1.1\r\nHost: www.example.com\r\n" +
		"User-Agent: Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0\r\n" +
		"Accept: text/html,application/xhtml+xml\r\nAccept-Language: en-US,en;q=0.5\r\n" +
		"Accept-Encoding: gzip, deflate\r\nConnection: keep-alive\r\nUpgrade-Insecure-Requests: 1\r\n\r\n")
	sniffer, _ := http.NewSnifferModule().NewSniffer()
	b.ReportAllocs()
	for b.Loop() {
		sniffer.Sniff(request)
	}
}
