package http_test

import (
	"errors"
	"testing"

	"github.com/The-Apranik/Apranik/module/sniffer/http"
	"github.com/The-Apranik/Apranik/spec"
)

func TestTheHostAndPortAreReportedUnchanged(t *testing.T) {
	request := "GET /index.html HTTP/1.1\r\nUser-Agent: test\r\nhost:   Example.com:8080  \r\n\r\n"

	result, err := http.NewSnifferModule().Sniff([]byte(request))
	if err != nil {
		t.Fatalf("Sniff: %v", err)
	}
	if result.Protocol != "http" || result.Domain != "Example.com" || result.Port != 8080 {
		t.Errorf("got %+v, want http Example.com 8080", result)
	}
}

func TestAnIncompleteHeaderAsksForMoreData(t *testing.T) {
	for _, prefix := range []string{"", "GE", "POST / HTTP/1.1\r\nHost: example"} {
		if _, err := http.NewSnifferModule().Sniff([]byte(prefix)); !errors.Is(err, spec.ErrSniffNeedMoreData) {
			t.Errorf("%q returned %v, want ErrSniffNeedMoreData", prefix, err)
		}
	}
}

func TestOtherProtocolsAreRefusedAtOnce(t *testing.T) {
	for _, payloadPrefix := range []string{"\x16\x03\x01", "SSH-2.0-OpenSSH_9.6\r\n", "GETX / HTTP/1.1\r\n\r\n"} {
		if _, err := http.NewSnifferModule().Sniff([]byte(payloadPrefix)); err == nil || errors.Is(err, spec.ErrSniffNeedMoreData) {
			t.Errorf("%q returned %v, want a refusal", payloadPrefix, err)
		}
	}
}

func TestAHostWithoutAPortReportsPortZero(t *testing.T) {
	for request, wantHost := range map[string]string{
		"GET / HTTP/1.1\r\nHost: example.com\r\n\r\n":   "example.com",
		"GET / HTTP/1.1\r\nHost: [2001:db8::1]\r\n\r\n": "2001:db8::1",
	} {
		result, err := http.NewSnifferModule().Sniff([]byte(request))
		if err != nil || result.Domain != wantHost || result.Port != 0 {
			t.Errorf("%q: got %+v, %v; want %s and port 0", request, result, err, wantHost)
		}
	}
}
