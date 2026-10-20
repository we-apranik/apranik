package match

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"regexp"
	"testing"
)

func TestDomainMatcher(t *testing.T) {
	matcher := NewDomainMatcher(DomainList{
		Exact:    []string{"Exact.Example.COM."},
		Suffixes: []string{"google.com", ".ir"},
		Keywords: []string{"Tracker"},
		Regexes:  []*regexp.Regexp{regexp.MustCompile(`^ads\d+\.`)},
	})

	cases := []struct {
		name string
		want bool
	}{
		{"exact.example.com", true},
		{"sub.exact.example.com", false},
		{"example.com", false},
		{"google.com", true},
		{"mail.google.com", true},
		{"a.b.mail.google.com", true},
		{"notgoogle.com", false},
		{"google.com.evil", false},
		{"digikala.ir", true},
		{"ir", true},
		{"iran.com", false},
		{"mytracker.net", true},
		{"ads12.example.net", true},
		{"ads.example.net", false},
		{"", false},
	}
	for _, testCase := range cases {
		if got := matcher.Match(testCase.name); got != testCase.want {
			t.Errorf("Match(%q) = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	for input, want := range map[string]string{
		"Example.COM.": "example.com",
		"example.com":  "example.com",
		"":             "",
	} {
		if got := NormalizeDomain(input); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEmptyDomainMatcherMatchesNothing(t *testing.T) {
	matcher := NewDomainMatcher(DomainList{})
	if matcher.Match("example.com") {
		t.Fatal("an empty matcher matched")
	}
	if matcher.Len() != 0 {
		t.Fatalf("Len = %d, want 0", matcher.Len())
	}
}

func TestPrefixMatcher(t *testing.T) {
	matcher := NewPrefixMatcher([]netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("10.20.0.0/16"), // inside the first: merged away
		netip.MustParsePrefix("192.168.1.0/24"),
		netip.MustParsePrefix("192.168.2.0/24"), // adjacent: merged
		netip.MustParsePrefix("203.0.113.7/32"),
		netip.MustParsePrefix("::ffff:100.64.0.0/106"), // IPv4-mapped: 100.64.0.0/10
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("fe80::1/128"),
	})

	cases := []struct {
		address string
		want    bool
	}{
		{"10.0.0.0", true},
		{"10.255.255.255", true},
		{"11.0.0.0", false},
		{"9.255.255.255", false},
		{"192.168.1.1", true},
		{"192.168.2.255", true},
		{"192.168.3.0", false},
		{"203.0.113.7", true},
		{"203.0.113.8", false},
		{"100.64.0.1", true},
		{"100.128.0.0", false},
		{"::ffff:10.1.2.3", true},
		{"2001:db8::1", true},
		{"2001:db8:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:db9::", false},
		{"fe80::1", true},
		{"fe80::1%eth0", true},
		{"fe80::2", false},
	}
	for _, testCase := range cases {
		if got := matcher.Contains(netip.MustParseAddr(testCase.address)); got != testCase.want {
			t.Errorf("Contains(%s) = %v, want %v", testCase.address, got, testCase.want)
		}
	}
	if matcher.Len() != 6 {
		t.Errorf("Len = %d, want 6 merged ranges", matcher.Len())
	}
}

func TestPrefixMatcherWholeSpace(t *testing.T) {
	matcher := NewPrefixMatcher([]netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/0"),
		netip.MustParsePrefix("255.255.255.0/24"),
		netip.MustParsePrefix("::/0"),
	})
	for _, address := range []string{"0.0.0.0", "255.255.255.255", "::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
		if !matcher.Contains(netip.MustParseAddr(address)) {
			t.Errorf("Contains(%s) = false, want true", address)
		}
	}
	if matcher.Len() != 2 {
		t.Errorf("Len = %d, want 2", matcher.Len())
	}
}

func TestPrefixMatcherAgreesWithPrefixContains(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	var prefixes []netip.Prefix
	for range 500 {
		prefixes = append(prefixes, randomIPv4Prefix(random))
	}
	matcher := NewPrefixMatcher(prefixes)

	for range 20000 {
		address := randomIPv4Address(random)
		want := false
		for _, prefix := range prefixes {
			if prefix.Contains(address) {
				want = true
				break
			}
		}
		if got := matcher.Contains(address); got != want {
			t.Fatalf("Contains(%s) = %v, want %v", address, got, want)
		}
	}
}

func TestPortMatcher(t *testing.T) {
	matcher := NewPortMatcher([]PortRange{{443, 443}, {1000, 2000}})
	for port, want := range map[uint16]bool{443: true, 444: false, 999: false, 1000: true, 1500: true, 2000: true, 2001: false} {
		if got := matcher.Contains(port); got != want {
			t.Errorf("Contains(%d) = %v, want %v", port, got, want)
		}
	}
}

func TestIsPrivateAddress(t *testing.T) {
	for address, want := range map[string]bool{
		"10.10.34.35":     true,
		"172.16.0.1":      true,
		"192.168.1.1":     true,
		"127.0.0.1":       true,
		"169.254.1.1":     true,
		"0.0.0.0":         true,
		"0.1.2.3":         true,
		"100.64.0.1":      true,
		"100.127.255.255": true,
		"198.18.0.1":      true,
		"192.0.2.1":       true,
		"224.0.0.1":       true,
		"255.255.255.255": true,
		"::1":             true,
		"::":              true,
		"fc00::1":         true,
		"fe80::1":         true,
		"ff02::1":         true,
		"::ffff:10.0.0.1": true,
		"8.8.8.8":         false,
		"100.128.0.1":     false,
		"185.143.233.2":   false,
		"2001:4860::8888": false,
	} {
		if got := IsPrivateAddress(netip.MustParseAddr(address)); got != want {
			t.Errorf("IsPrivateAddress(%s) = %v, want %v", address, got, want)
		}
	}
}

func randomIPv4Address(random *rand.Rand) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(random.Uint32()), byte(random.Uint32()), byte(random.Uint32()), byte(random.Uint32())})
}

func randomIPv4Prefix(random *rand.Rand) netip.Prefix {
	return netip.PrefixFrom(randomIPv4Address(random), 8+random.IntN(25)).Masked()
}

func largeDomainMatcher(size int) (*DomainMatcher, []string) {
	names := make([]string, size)
	for index := range names {
		names[index] = fmt.Sprintf("site%d.example%d.com", index, index%97)
	}
	return NewDomainMatcher(DomainList{Suffixes: names}), names
}

func BenchmarkDomainMatcherSuffixHit(b *testing.B) {
	matcher, names := largeDomainMatcher(100_000)
	name := "cdn.static." + names[len(names)/2]
	b.ReportAllocs()
	for b.Loop() {
		if !matcher.Match(name) {
			b.Fatal("no match")
		}
	}
}

func BenchmarkDomainMatcherSuffixMiss(b *testing.B) {
	matcher, _ := largeDomainMatcher(100_000)
	b.ReportAllocs()
	for b.Loop() {
		if matcher.Match("cdn.static.unlisted-site.example.org") {
			b.Fatal("matched")
		}
	}
}

func BenchmarkDomainMatcherKeywords(b *testing.B) {
	keywords := make([]string, 200)
	for index := range keywords {
		keywords[index] = fmt.Sprintf("keyword%d", index)
	}
	matcher := NewDomainMatcher(DomainList{Keywords: keywords})
	b.ReportAllocs()
	for b.Loop() {
		matcher.Match("cdn.static.unlisted-site.example.org")
	}
}

func BenchmarkNormalizeDomainLowercase(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		NormalizeDomain("cdn.static.example.org.")
	}
}

func BenchmarkPrefixMatcherIPv4(b *testing.B) {
	random := rand.New(rand.NewPCG(3, 4))
	prefixes := make([]netip.Prefix, 10_000)
	for index := range prefixes {
		prefixes[index] = randomIPv4Prefix(random)
	}
	matcher := NewPrefixMatcher(prefixes)
	addresses := make([]netip.Addr, 1024)
	for index := range addresses {
		addresses[index] = randomIPv4Address(random)
	}
	b.ReportAllocs()
	index := 0
	for b.Loop() {
		matcher.Contains(addresses[index&1023])
		index++
	}
}

func BenchmarkPrefixMatcherIPv6(b *testing.B) {
	random := rand.New(rand.NewPCG(5, 6))
	prefixes := make([]netip.Prefix, 10_000)
	for index := range prefixes {
		var bytes [16]byte
		for position := range bytes {
			bytes[position] = byte(random.Uint32())
		}
		prefixes[index] = netip.PrefixFrom(netip.AddrFrom16(bytes), 16+random.IntN(48)).Masked()
	}
	matcher := NewPrefixMatcher(prefixes)
	address := netip.MustParseAddr("2001:db8::1")
	b.ReportAllocs()
	for b.Loop() {
		matcher.Contains(address)
	}
}

func BenchmarkIsPrivateAddress(b *testing.B) {
	address := netip.MustParseAddr("185.143.233.2")
	b.ReportAllocs()
	for b.Loop() {
		IsPrivateAddress(address)
	}
}
