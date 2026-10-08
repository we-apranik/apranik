package clash_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/api/clash"
	"github.com/The-Apranik/Apranik/spec"
)

func settingsFrom(t *testing.T, text string) *clash.Module {
	t.Helper()
	module := clash.NewAPIModule().(*clash.Module)
	if err := json.Unmarshal([]byte(text), module); err != nil {
		t.Fatal(err)
	}
	return module
}

func TestTheModuleIsNamedClashAndRegistersAsAnAPI(t *testing.T) {
	var module spec.APIModule = clash.NewAPIModule()
	if module.Info().Name != "clash" || module.Info().Description == "" {
		t.Errorf("Info = %+v", module.Info())
	}
}

func TestValidSettingsAreAccepted(t *testing.T) {
	module := settingsFrom(t, `{
		"listen": "127.0.0.1", "port": 9090, "secret": "x",
		"allow_origins": ["https://metacubex.github.io", "http://127.0.0.1:3000", "*"],
		"log_buffer_records": 512,
		"delay_test": {"url": "http://example.com/204", "timeout": "3s", "expected_status": "204"}
	}`)
	if err := module.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := module.NewAPIServer(); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsRefuseWhatTheServerCannotRunWith(t *testing.T) {
	for name, test := range map[string]struct{ settings, problem string }{
		"no listen":                    {`{"port": 9090, "secret": "x"}`, "listen is required"},
		"listen is a name":             {`{"listen": "localhost", "port": 9090, "secret": "x"}`, "not an IP address"},
		"listen has a zone":            {`{"listen": "fe80::1%eth0", "port": 9090, "secret": "x"}`, "not an IP address"},
		"no port":                      {`{"listen": "127.0.0.1", "secret": "x"}`, "port 0"},
		"port too big":                 {`{"listen": "127.0.0.1", "port": 70000, "secret": "x"}`, "port 70000"},
		"no secret":                    {`{"listen": "127.0.0.1", "port": 9090}`, "secret is required"},
		"an origin with a path":        {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "allow_origins": ["https://a.example/app"]}`, "allow_origins"},
		"an origin without a scheme":   {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "allow_origins": ["a.example"]}`, "allow_origins"},
		"a written zero buffer":        {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "log_buffer_records": 0}`, "log_buffer_records"},
		"a negative buffer":            {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "log_buffer_records": -5}`, "log_buffer_records"},
		"a buffer above the maximum":   {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "log_buffer_records": 100001}`, "log_buffer_records"},
		"a delay url that is a path":   {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "delay_test": {"url": "/204"}}`, "delay_test url"},
		"a delay url that is ftp":      {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "delay_test": {"url": "ftp://example.com/x"}}`, "delay_test url"},
		"a relative dashboard folder":  {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "external_ui": "ui"}`, "external_ui"},
		"a written zero delay timeout": {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "delay_test": {"timeout": "0s"}}`, "delay_test timeout"},
	} {
		err := settingsFrom(t, test.settings).Validate()
		if err == nil || !strings.Contains(err.Error(), test.problem) {
			t.Errorf("%s: Validate = %v, want an error naming %q", name, err, test.problem)
		}
	}
}

func TestANewServerIsRefusedForInvalidSettings(t *testing.T) {
	if _, err := clash.NewAPIModule().NewAPIServer(); err == nil {
		t.Error("a server was built from empty settings")
	}
}

func TestAnErrorNeverCarriesTheSecret(t *testing.T) {
	module := settingsFrom(t, `{"listen": "127.0.0.1", "port": 0, "secret": "do-not-print-me"}`)
	if err := module.Validate(); err == nil || strings.Contains(err.Error(), "do-not-print-me") {
		t.Errorf("Validate = %v", err)
	}
	if strings.Contains(module.Secret.String(), "do-not-print-me") {
		t.Error("the secret prints itself")
	}
	var _ types.Secret = module.Secret
}

func TestWarningsNameWhatMakesTheAPIEasyToReach(t *testing.T) {
	cases := map[string]struct {
		settings string
		wantWarn string
	}{
		"loopback is quiet":   {`{"listen": "127.0.0.1", "port": 9090, "secret": "x"}`, ""},
		"ipv6 loopback":       {`{"listen": "::1", "port": 9090, "secret": "x"}`, ""},
		"all interfaces":      {`{"listen": "0.0.0.0", "port": 9090, "secret": "x"}`, "not on this machine"},
		"a LAN address":       {`{"listen": "192.168.1.5", "port": 9090, "secret": "x"}`, "not on this machine"},
		"any origin":          {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "allow_origins": ["*"]}`, "any web page"},
		"a very large buffer": {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "log_buffer_records": 50000}`, "each /logs client"},
		"a modest buffer":     {`{"listen": "127.0.0.1", "port": 9090, "secret": "x", "log_buffer_records": 5000}`, ""},
	}
	for name, test := range cases {
		warnings := settingsFrom(t, test.settings).Warnings()
		if test.wantWarn == "" {
			if len(warnings) != 0 {
				t.Errorf("%s: warnings = %v, want none", name, warnings)
			}
			continue
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], test.wantWarn) {
			t.Errorf("%s: warnings = %v, want one containing %q", name, warnings, test.wantWarn)
		}
	}
}
