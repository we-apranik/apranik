package dataset

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/route/dataset/geodata/geodatatest"
)

func writeFixtures(t *testing.T) (sitePath, ipPath string) {
	t.Helper()
	directory := t.TempDir()
	sitePath = filepath.Join(directory, "geosite.dat")
	ipPath = filepath.Join(directory, "geoip.dat")
	site := geodatatest.Site(map[string][]geodatatest.Domain{
		"IR":  {{Type: geodatatest.Suffix, Value: "ir"}},
		"ADS": {{Type: geodatatest.Exact, Value: "ads.example.com"}},
	})
	ip := geodatatest.IP(map[string][]netip.Prefix{"IR": {netip.MustParsePrefix("185.143.232.0/22")}})
	if err := os.WriteFile(sitePath, site, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipPath, ip, 0o600); err != nil {
		t.Fatal(err)
	}
	return sitePath, ipPath
}

func TestLoadAll(t *testing.T) {
	sitePath, ipPath := writeFixtures(t)
	declarations := []blueprint.Dataset{
		{Tag: "ir-domains", Kind: blueprint.DatasetKindDomain, File: sitePath, Format: "geodata", Category: "ir"},
		{Tag: "ads", Kind: blueprint.DatasetKindDomain, File: sitePath, Format: "geodata", Category: "ads"},
		{Tag: "ir-ips", Kind: blueprint.DatasetKindIP, File: ipPath, Format: "geodata", Category: "ir"},
	}
	loaded, errs := LoadAll(declarations)
	if err := Joined(declarations, errs); err != nil {
		t.Fatal(err)
	}
	if !loaded["ir-domains"].Domains.Match("digikala.ir") {
		t.Error("ir-domains does not match digikala.ir")
	}
	if !loaded["ads"].Domains.Match("ads.example.com") || loaded["ads"].Domains.Match("digikala.ir") {
		t.Error("ads matches the wrong names")
	}
	if !loaded["ir-ips"].IPs.Contains(netip.MustParseAddr("185.143.233.2")) {
		t.Error("ir-ips does not contain 185.143.233.2")
	}
	if loaded["ir-ips"].Len() != 1 {
		t.Errorf("ir-ips Len = %d, want 1", loaded["ir-ips"].Len())
	}
}

func TestLoadAllReportsEachFailureAtItsDeclaration(t *testing.T) {
	sitePath, ipPath := writeFixtures(t)
	declarations := []blueprint.Dataset{
		{Tag: "fine", Kind: blueprint.DatasetKindDomain, File: sitePath, Format: "geodata", Category: "ir"},
		{Tag: "missing-file", Kind: blueprint.DatasetKindDomain, File: filepath.Join(t.TempDir(), "absent.dat"), Format: "geodata", Category: "ir"},
		{Tag: "missing-category", Kind: blueprint.DatasetKindDomain, File: sitePath, Format: "geodata", Category: "cn"},
		{Tag: "wrong-kind", Kind: blueprint.DatasetKindDomain, File: ipPath, Format: "geodata", Category: "ir"},
		{Tag: "wrong-kind-ip", Kind: blueprint.DatasetKindIP, File: sitePath, Format: "geodata", Category: "ir"},
		{Tag: "ip-attribute", Kind: blueprint.DatasetKindIP, File: ipPath, Format: "geodata", Category: "ir", Attribute: "cn"},
		{Tag: "unknown-format", Kind: blueprint.DatasetKindDomain, File: sitePath, Format: "srs"},
	}
	loaded, errs := LoadAll(declarations)

	wantErrorContaining := []string{"", "absent.dat", "\"cn\"", "holds IP ranges", "holds domains", "attribute", "unknown format"}
	for index, want := range wantErrorContaining {
		err := errs[index]
		switch {
		case want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", declarations[index].Tag, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%s: got %v, want an error containing %q", declarations[index].Tag, err, want)
		}
	}
	if len(loaded) != 1 {
		t.Errorf("loaded %d datasets, want only the fine one", len(loaded))
	}
	if joined := Joined(declarations, errs); joined == nil || !strings.Contains(joined.Error(), `dataset "wrong-kind"`) {
		t.Errorf("Joined = %v, want every failure named", joined)
	}
}
