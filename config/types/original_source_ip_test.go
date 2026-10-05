package types

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/spec"
)

func decodeOriginalSourceIP(t *testing.T, text string) OriginalSourceIP {
	t.Helper()
	settings := DefaultOriginalSourceIP()
	if err := json.Unmarshal([]byte(text), &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestOriginalSourceIPDefaults(t *testing.T) {
	settings := decodeOriginalSourceIP(t, `{}`)
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(settings.Headers) != 1 || settings.Headers[0] != "X-Forwarded-For" || settings.Disabled || !settings.Unverified() {
		t.Errorf("defaults = %+v", settings)
	}
}

func TestOriginalSourceIPHeadersReplaceTheDefault(t *testing.T) {
	settings := decodeOriginalSourceIP(t, `{"headers": ["CF-Connecting-IP"]}`)
	if len(settings.Headers) != 1 || settings.Headers[0] != "CF-Connecting-IP" {
		t.Errorf("headers = %v", settings.Headers)
	}
}

func TestOriginalSourceIPValidate(t *testing.T) {
	for name, tc := range map[string]struct {
		text    string
		wantErr string
	}{
		"empty headers":                     {`{"headers": []}`, "at least one header"},
		"bad header name":                   {`{"headers": ["X Forwarded"]}`, "not an HTTP header name"},
		"empty proof name":                  {`{"verification": {"proof_header": {"name": ""}}}`, "proof_header.name"},
		"proof name equal to a read header": {`{"verification": {"proof_header": {"name": "x-forwarded-for"}}}`, "also in headers"},
		"valid":                             {`{"verification": {"trusted_proxy_addresses": ["10.0.0.2"], "proof_header": {"name": "X-Edge-Secret", "value": "s"}}}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			settings := decodeOriginalSourceIP(t, tc.text)
			err := settings.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestOriginalSourceIPWarnings(t *testing.T) {
	broad := decodeOriginalSourceIP(t, `{"verification": {"trusted_proxy_addresses": ["0.0.0.0/0"]}}`)
	if warnings := broad.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "covers every address") {
		t.Errorf("broad range warnings = %v", warnings)
	}
	unused := decodeOriginalSourceIP(t, `{"disabled": true, "verification": {"trusted_proxy_addresses": ["10.0.0.2"]}}`)
	if warnings := unused.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "unused") {
		t.Errorf("disabled with verification warnings = %v", warnings)
	}
	quiet := decodeOriginalSourceIP(t, `{"verification": {"trusted_proxy_addresses": ["10.0.0.2"]}}`)
	if warnings := quiet.Warnings(); len(warnings) != 0 || quiet.Unverified() {
		t.Errorf("a verified setting warned: %v", warnings)
	}
}

func TestOriginalSourceIPProofValueIsASecret(t *testing.T) {
	settings := decodeOriginalSourceIP(t, `{"verification": {"proof_header": {"name": "X-Edge-Secret", "value": "hunter2"}}}`)
	if strings.Contains(settings.Verification.ProofHeader.Value.String(), "hunter2") {
		t.Error("the proof value prints itself")
	}
}

func listenAt(t *testing.T, address string) spec.Endpoint {
	t.Helper()
	endpoint, err := spec.NewEndpoint(netip.MustParseAddr(address), 8443)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func TestOriginalSourceIPListenAddressWarning(t *testing.T) {
	unverified := decodeOriginalSourceIP(t, `{}`)
	verified := decodeOriginalSourceIP(t, `{"verification": {"trusted_proxy_addresses": ["10.0.0.2"]}}`)
	disabled := decodeOriginalSourceIP(t, `{"disabled": true}`)
	for name, tc := range map[string]struct {
		settings OriginalSourceIP
		listen   string
		want     bool
	}{
		"unverified on loopback":         {unverified, "127.0.0.1", false},
		"unverified on IPv6 loopback":    {unverified, "::1", false},
		"unverified on a public address": {unverified, "203.0.113.9", true},
		"unverified on every interface":  {unverified, "0.0.0.0", true},
		"verified on every interface":    {verified, "0.0.0.0", false},
		"disabled on every interface":    {disabled, "0.0.0.0", false},
	} {
		got := tc.settings.WarningsForListenAddress("ws", listenAt(t, tc.listen))
		if (len(got) > 0) != tc.want {
			t.Errorf("%s: warnings = %v, want a warning: %v", name, got, tc.want)
		}
	}
}

func TestOriginalSourceIPResolverIsNilWhenDisabled(t *testing.T) {
	disabled := decodeOriginalSourceIP(t, `{"disabled": true}`)
	if disabled.NewResolver() != nil {
		t.Error("a disabled setting built a resolver")
	}
	enabled := decodeOriginalSourceIP(t, `{"verification": {"proof_header": {"name": "X-Edge-Secret", "value": "s"}}}`)
	header := http.Header{"X-Forwarded-For": {"5.6.7.8"}, "X-Edge-Secret": {"s"}}
	if source, ok := enabled.NewResolver().Resolve(header, listenAt(t, "10.0.0.2")); !ok || source.Addr() != netip.MustParseAddr("5.6.7.8") {
		t.Errorf("Resolve = %v, %v", source, ok)
	}
}
