package types

import (
	"encoding/json"
	"net/netip"
	"testing"
)

func TestAddressRangesContain(t *testing.T) {
	ranges, err := ParseAddressRanges("10.0.0.2", "104.16.0.0/13", "::1", "2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	for address, want := range map[string]bool{
		"10.0.0.2":          true,
		"10.0.0.3":          false,
		"104.23.255.255":    true,
		"104.24.0.0":        false,
		"::1":               true,
		"2001:db8::5":       true,
		"2001:db9::5":       false,
		"::ffff:10.0.0.2":   true,
		"::ffff:104.16.0.1": true,
	} {
		if got := ranges.Contains(netip.MustParseAddr(address)); got != want {
			t.Errorf("Contains(%s) = %v, want %v", address, got, want)
		}
	}
}

func TestAddressRangesDecodeAndRefuseWhatIsNotAnAddress(t *testing.T) {
	var ranges AddressRanges
	if err := json.Unmarshal([]byte(`["127.0.0.0/8", "::1"]`), &ranges); err != nil || len(ranges) != 2 {
		t.Fatalf("decoded %v, error %v", ranges, err)
	}
	for _, bad := range []string{`["nope"]`, `["10.0.0.0/33"]`, `"10.0.0.1"`, `[1]`} {
		if err := json.Unmarshal([]byte(bad), &ranges); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestAddressRangesDecodeReplacesADefault(t *testing.T) {
	ranges, _ := ParseAddressRanges("127.0.0.0/8")
	if err := json.Unmarshal([]byte(`["10.0.0.2"]`), &ranges); err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 || ranges.Contains(netip.MustParseAddr("127.0.0.1")) {
		t.Errorf("the default survived: %v", ranges)
	}
}

func TestAddressRangesCoverEverything(t *testing.T) {
	everything, _ := ParseAddressRanges("0.0.0.0/0")
	someone, _ := ParseAddressRanges("10.0.0.0/8")
	if !everything.CoversEverything() || someone.CoversEverything() {
		t.Error("CoversEverything is wrong")
	}
}
