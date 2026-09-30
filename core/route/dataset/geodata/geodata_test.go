package geodata

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"testing"

	"github.com/The-Apranik/Apranik/core/route/dataset/geodata/geodatatest"
	"github.com/The-Apranik/Apranik/core/route/match"
)

func testSiteFile() []byte {
	return geodatatest.Site(map[string][]geodatatest.Domain{
		"IR": {
			{Type: geodatatest.Suffix, Value: "ir"},
			{Type: geodatatest.Exact, Value: "digikala.com"},
			{Type: geodatatest.Keyword, Value: "snapp"},
			{Type: geodatatest.Regex, Value: `^cdn\d+\.aparat\.com$`},
		},
		"GOOGLE": {
			{Type: geodatatest.Suffix, Value: "google.com"},
			{Type: geodatatest.Suffix, Value: "google.cn", Attributes: []string{"cn"}},
		},
	})
}

func TestReadDomains(t *testing.T) {
	list, err := ReadDomains(testSiteFile(), "ir", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(list.Suffixes, []string{"ir"}) || !slices.Equal(list.Exact, []string{"digikala.com"}) ||
		!slices.Equal(list.Keywords, []string{"snapp"}) || len(list.Regexes) != 1 {
		t.Fatalf("got %+v", list)
	}

	matcher := match.NewDomainMatcher(list)
	for name, want := range map[string]bool{
		"digikala.ir": true, "digikala.com": true, "www.digikala.com": false,
		"snappfood.net": true, "cdn12.aparat.com": true, "example.com": false,
	} {
		if got := matcher.Match(name); got != want {
			t.Errorf("Match(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestReadDomainsWithAttribute(t *testing.T) {
	list, err := ReadDomains(testSiteFile(), "google", "CN")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(list.Suffixes, []string{"google.cn"}) {
		t.Fatalf("got %v, want only the entry carrying cn", list.Suffixes)
	}
}

func TestReadPrefixes(t *testing.T) {
	file := geodatatest.IP(map[string][]netip.Prefix{
		"IR":      {netip.MustParsePrefix("185.143.232.0/22"), netip.MustParsePrefix("2a01:5ec0::/29")},
		"PRIVATE": {netip.MustParsePrefix("10.0.0.0/8")},
	})
	prefixes, err := ReadPrefixes(file, "ir")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("185.143.232.0/22"), netip.MustParsePrefix("2a01:5ec0::/29")}
	if !slices.Equal(prefixes, want) {
		t.Fatalf("got %v, want %v", prefixes, want)
	}
}

func TestMissingCategory(t *testing.T) {
	_, err := ReadDomains(testSiteFile(), "cn", "")
	if !errors.Is(err, ErrCategoryNotFound) {
		t.Fatalf("got %v, want ErrCategoryNotFound", err)
	}
}

func TestWrongKind(t *testing.T) {
	ipFile := geodatatest.IP(map[string][]netip.Prefix{"IR": {netip.MustParsePrefix("185.143.232.0/22")}})
	if _, err := ReadDomains(ipFile, "ir", ""); !errors.Is(err, ErrWrongKind) {
		t.Errorf("reading geoip as domains: got %v, want ErrWrongKind", err)
	}
	if _, err := ReadPrefixes(testSiteFile(), "ir"); !errors.Is(err, ErrWrongKind) {
		t.Errorf("reading geosite as prefixes: got %v, want ErrWrongKind", err)
	}
}

func TestMalformedFiles(t *testing.T) {
	for name, file := range map[string][]byte{
		"truncated":      testSiteFile()[:7],
		"field number 0": {0x02, 0x00},
		"bad wire type":  {0x0f},
		"length too big": {0x0a, 0xff, 0x01},
	} {
		if _, err := ReadDomains(file, "ir", ""); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestBadRegexIsReported(t *testing.T) {
	file := geodatatest.Site(map[string][]geodatatest.Domain{"X": {{Type: geodatatest.Regex, Value: "("}}})
	if _, err := ReadDomains(file, "x", ""); err == nil {
		t.Fatal("no error for a regex that does not compile")
	}
}

// Set APRANIK_GEOSITE to a real geosite.dat to check the decoder against it.
func TestRealGeositeFile(t *testing.T) {
	path := os.Getenv("APRANIK_GEOSITE")
	if path == "" {
		t.Skip("APRANIK_GEOSITE is not set")
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := ReadDomains(file, "category-ads-all", "")
	if err != nil {
		t.Fatal(err)
	}
	if match.NewDomainMatcher(list).Len() == 0 {
		t.Fatal("category-ads-all is empty")
	}
}

func FuzzReadDomains(f *testing.F) {
	f.Add(testSiteFile(), "ir")
	f.Add([]byte{0x0a, 0x02, 0x0a, 0x00}, "")
	f.Fuzz(func(t *testing.T, file []byte, category string) {
		ReadDomains(file, category, "cn")
		ReadPrefixes(file, category)
	})
}

func largeSiteFile(categories, domainsPerCategory int) []byte {
	all := make(map[string][]geodatatest.Domain, categories)
	for category := range categories {
		domains := make([]geodatatest.Domain, domainsPerCategory)
		for index := range domains {
			domains[index] = geodatatest.Domain{Type: geodatatest.Suffix, Value: fmt.Sprintf("site%d.category%d.com", index, category)}
		}
		all[fmt.Sprintf("CATEGORY-%d", category)] = domains
	}
	return geodatatest.Site(all)
}

// A real geosite.dat has about 1,500 categories; routing loads a few.
func BenchmarkReadOneCategoryOfMany(b *testing.B) {
	file := largeSiteFile(1500, 200)
	b.SetBytes(int64(len(file)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ReadDomains(file, "category-750", ""); err != nil {
			b.Fatal(err)
		}
	}
}
