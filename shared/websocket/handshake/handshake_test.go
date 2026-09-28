package handshake_test

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/shared/websocket/handshake"
	"github.com/The-Apranik/Apranik/spec"
)

func TestTheDefaultHostIsTheServersAddress(t *testing.T) {
	domain := func(host string, port uint16) spec.Destination {
		server, _ := spec.NewDestinationFromDomain(host, port)
		return server
	}
	address := func(ip string, port uint16) spec.Destination {
		server, _ := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
		return server
	}
	for want, server := range map[string]spec.Destination{
		"example.com":      domain("example.com", 443),
		"example.com:8443": domain("example.com", 8443),
		"192.0.2.1":        address("192.0.2.1", 80),
		"[2001:db8::1]":    address("2001:db8::1", 443),
		"[2001:db8::1]:81": address("2001:db8::1", 81),
	} {
		if got := handshake.HostHeaderFor(server); got != want {
			t.Errorf("Host for %s is %q, want %q", server, got, want)
		}
	}
}

func TestHostMatchingIgnoresCaseAndPort(t *testing.T) {
	for _, tc := range []struct {
		request, required string
		want              bool
	}{
		{"cdn.example", "cdn.example", true},
		{"CDN.Example:443", "cdn.example", true},
		{"[2001:db8::1]:443", "2001:db8::1", true},
		{"other.example", "cdn.example", false},
		{"cdn.example.evil", "cdn.example", false},
	} {
		if got := handshake.HostMatches(tc.request, tc.required); got != tc.want {
			t.Errorf("HostMatches(%q, %q) = %v", tc.request, tc.required, got)
		}
	}
}

func TestConnectionMayBeATokenList(t *testing.T) {
	header := http.Header{"Connection": {"keep-alive, Upgrade"}}
	if !handshake.HeaderContainsToken(header, "Connection", "upgrade") {
		t.Error("Upgrade in a list was not found")
	}
	if handshake.HeaderContainsToken(header, "Connection", "close") {
		t.Error("a token that is absent was found")
	}
}

func TestRequestHeaderLinesAreSortedWithADefaultUserAgent(t *testing.T) {
	lines := handshake.RequestHeaderLines(map[string]string{"X-B": "2", "X-A": "1"})
	if !strings.HasPrefix(lines, "X-A: 1\r\nX-B: 2\r\nUser-Agent: ") {
		t.Errorf("lines %q", lines)
	}
	if lines := handshake.RequestHeaderLines(map[string]string{"user-agent": "mine"}); lines != "user-agent: mine\r\n" {
		t.Errorf("a given User-Agent was not kept alone: %q", lines)
	}
}

func TestValidation(t *testing.T) {
	owned := []string{"Host", "Upgrade"}
	for name, err := range map[string]error{
		"path without slash": handshake.ValidatePath("up"),
		"path with space":    handshake.ValidatePath("/a b"),
		"host with slash":    handshake.ValidateHost("a/b"),
		"bad header name":    handshake.ValidateHeaders(map[string]string{"X Bad": "1"}, owned),
		"line break":         handshake.ValidateHeaders(map[string]string{"X-A": "1\r\nX-B: 2"}, owned),
		"owned header":       handshake.ValidateHeaders(map[string]string{"upgrade": "x"}, owned),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := handshake.ValidateHeaders(map[string]string{"X-Fine": "yes"}, owned); err != nil {
		t.Errorf("a fine header was refused: %v", err)
	}
}
