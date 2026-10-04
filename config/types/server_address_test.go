package types_test

import (
	"encoding/json"
	"testing"

	"github.com/The-Apranik/Apranik/config/types"
)

func TestServerAddress(t *testing.T) {
	for _, tc := range []struct {
		text, want  string
		defaultPort uint16
	}{
		{"8.8.8.8", "8.8.8.8:853", 853},
		{"8.8.8.8:53", "8.8.8.8:53", 853},
		{"Dns.Google.", "dns.google:443", 443},
		{"dns.google:8443", "dns.google:8443", 443},
		{"2001:db8::1", "[2001:db8::1]:53", 53},
		{"[2001:db8::1]", "[2001:db8::1]:53", 53},
		{"[2001:db8::1]:5353", "[2001:db8::1]:5353", 53},
		{"::ffff:192.0.2.1", "192.0.2.1:53", 53},
	} {
		var address types.ServerAddress
		if err := json.Unmarshal([]byte(`"`+tc.text+`"`), &address); err != nil {
			t.Errorf("%s: %v", tc.text, err)
			continue
		}
		destination, err := address.Destination(tc.defaultPort)
		if err != nil || destination.String() != tc.want {
			t.Errorf("%s: got %v, %v; want %s", tc.text, destination, err, tc.want)
		}
	}
	for _, invalid := range []string{"", "host:0", "host:70000", "a b.example", "evil\r\n.example", "a..b", "fe80::1%eth0", "https://dns.google"} {
		if _, err := types.ParseServerAddress(invalid); err == nil {
			t.Errorf("%q accepted", invalid)
		}
	}
	var address types.ServerAddress
	if err := json.Unmarshal([]byte(`53`), &address); err == nil {
		t.Error("a bare number was accepted")
	}
}
