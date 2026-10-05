package originalsourceip

import (
	"net/http"
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/spec"
)

func endpoint(address string, port uint16) spec.Endpoint {
	result, err := spec.NewEndpoint(netip.MustParseAddr(address), port)
	if err != nil {
		panic(err)
	}
	return result
}

func prefixes(entries ...string) []netip.Prefix {
	result := make([]netip.Prefix, len(entries))
	for index, entry := range entries {
		result[index] = netip.MustParsePrefix(entry)
	}
	return result
}

func headers(lines ...string) http.Header {
	header := http.Header{}
	for index := 0; index+1 < len(lines); index += 2 {
		header.Add(lines[index], lines[index+1])
	}
	return header
}

func TestResolve(t *testing.T) {
	forwarded := []string{"X-Forwarded-For"}
	for _, tc := range []struct {
		name   string
		config Config
		header http.Header
		below  spec.Endpoint
		want   string // empty: ignored
	}{
		{"single entry", Config{Headers: forwarded}, headers("X-Forwarded-For", "5.6.7.8"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"a forged prefix is ignored", Config{Headers: forwarded}, headers("X-Forwarded-For", "1.2.3.4, 5.6.7.8"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"no list: right-most even behind two proxies", Config{Headers: forwarded}, headers("X-Forwarded-For", "5.6.7.8, 104.16.0.1"), endpoint("10.0.0.2", 1), "104.16.0.1:0"},
		{"the walk skips listed proxies", Config{Headers: forwarded, TrustedProxies: prefixes("10.0.0.2/32", "104.16.0.0/13")},
			headers("X-Forwarded-For", "5.6.7.8, 104.16.0.1"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"the walk ignores a forged prefix", Config{Headers: forwarded, TrustedProxies: prefixes("10.0.0.2/32", "104.16.0.0/13")},
			headers("X-Forwarded-For", "9.9.9.9, 5.6.7.8, 104.16.0.1"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"every entry listed: the left-most", Config{Headers: forwarded, TrustedProxies: prefixes("10.0.0.0/8")},
			headers("X-Forwarded-For", "10.0.0.9, 10.0.0.8"), endpoint("10.0.0.2", 1), "10.0.0.9:0"},
		{"an unlisted peer is ignored", Config{Headers: forwarded, TrustedProxies: prefixes("10.0.0.2/32")},
			headers("X-Forwarded-For", "5.6.7.8"), endpoint("203.0.113.9", 1), ""},
		{"an IPv4-mapped peer matches its IPv4 range", Config{Headers: forwarded, TrustedProxies: prefixes("10.0.0.2/32")},
			headers("X-Forwarded-For", "5.6.7.8"), endpoint("::ffff:10.0.0.2", 1), "5.6.7.8:0"},
		{"the entry's port is kept", Config{Headers: forwarded}, headers("X-Forwarded-For", "5.6.7.8:5678"), endpoint("10.0.0.2", 1), "5.6.7.8:5678"},
		{"bracketed IPv6 with a port", Config{Headers: forwarded}, headers("X-Forwarded-For", "[2001:db8::1]:80"), endpoint("10.0.0.2", 1), "[2001:db8::1]:80"},
		{"bracketed IPv6 without a port", Config{Headers: forwarded}, headers("X-Forwarded-For", "[2001:db8::1]"), endpoint("10.0.0.2", 1), "[2001:db8::1]:0"},
		{"IPv4-mapped entry", Config{Headers: forwarded}, headers("X-Forwarded-For", "::ffff:5.6.7.8"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"repeated lines are one list", Config{Headers: forwarded}, headers("X-Forwarded-For", "1.2.3.4", "X-Forwarded-For", "5.6.7.8"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"absent header", Config{Headers: forwarded}, headers(), endpoint("10.0.0.2", 1), ""},
		{"an entry that is not an address", Config{Headers: forwarded}, headers("X-Forwarded-For", "5.6.7.8, unknown"), endpoint("10.0.0.2", 1), ""},
		{"an empty entry", Config{Headers: forwarded}, headers("X-Forwarded-For", "5.6.7.8,,"), endpoint("10.0.0.2", 1), ""},
		{"a zone is refused", Config{Headers: forwarded}, headers("X-Forwarded-For", "fe80::1%eth0"), endpoint("10.0.0.2", 1), ""},
		{"the first listed header present wins", Config{Headers: []string{"CF-Connecting-IP", "X-Forwarded-For"}},
			headers("X-Forwarded-For", "1.1.1.1", "CF-Connecting-IP", "5.6.7.8"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"the next header is read when the first is absent", Config{Headers: []string{"CF-Connecting-IP", "X-Forwarded-For"}},
			headers("X-Forwarded-For", "1.1.1.1"), endpoint("10.0.0.2", 1), "1.1.1.1:0"},
		{"a bad value does not fall through to the next header", Config{Headers: []string{"CF-Connecting-IP", "X-Forwarded-For"}},
			headers("CF-Connecting-IP", "garbage", "X-Forwarded-For", "1.1.1.1"), endpoint("10.0.0.2", 1), ""},
		{"proof present with the right value", Config{Headers: forwarded, ProofHeaderName: "X-Edge-Secret", ProofHeaderValue: "s3cret"},
			headers("X-Forwarded-For", "5.6.7.8", "X-Edge-Secret", "s3cret"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"proof with the wrong value", Config{Headers: forwarded, ProofHeaderName: "X-Edge-Secret", ProofHeaderValue: "s3cret"},
			headers("X-Forwarded-For", "5.6.7.8", "X-Edge-Secret", "wrong"), endpoint("10.0.0.2", 1), ""},
		{"proof missing", Config{Headers: forwarded, ProofHeaderName: "X-Edge-Secret", ProofHeaderValue: "s3cret"},
			headers("X-Forwarded-For", "5.6.7.8"), endpoint("10.0.0.2", 1), ""},
		{"proof by presence only", Config{Headers: forwarded, ProofHeaderName: "X-Edge-Secret"},
			headers("X-Forwarded-For", "5.6.7.8", "X-Edge-Secret", "anything"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
		{"both checks must hold: listed peer, wrong proof", Config{Headers: forwarded, TrustedProxies: prefixes("10.0.0.2/32"), ProofHeaderName: "X-Edge-Secret", ProofHeaderValue: "s3cret"},
			headers("X-Forwarded-For", "5.6.7.8", "X-Edge-Secret", "wrong"), endpoint("10.0.0.2", 1), ""},
		{"both checks must hold: right proof, unlisted peer", Config{Headers: forwarded, TrustedProxies: prefixes("10.0.0.2/32"), ProofHeaderName: "X-Edge-Secret", ProofHeaderValue: "s3cret"},
			headers("X-Forwarded-For", "5.6.7.8", "X-Edge-Secret", "s3cret"), endpoint("203.0.113.9", 1), ""},
		{"header names are case-insensitive", Config{Headers: []string{"x-forwarded-for"}}, headers("X-Forwarded-For", "5.6.7.8"), endpoint("10.0.0.2", 1), "5.6.7.8:0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := New(tc.config).Resolve(tc.header, tc.below)
			switch {
			case tc.want == "" && ok:
				t.Fatalf("believed the request: %s", got)
			case tc.want != "" && !ok:
				t.Fatalf("ignored the request, want %s", tc.want)
			case ok && got.String() != tc.want:
				t.Fatalf("source = %s, want %s", got, tc.want)
			}
		})
	}
}
