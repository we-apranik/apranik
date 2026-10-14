package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/config"
	"github.com/we-apranik/apranik/core/registry"
	"github.com/we-apranik/apranik/spec"
)

// A module that only reads its settings: no server, no listener.
type fakeAPI struct {
	Port     int    `json:"port"`
	Secret   string `json:"secret"`
	Complain bool   `json:"complain"`
}

func (a *fakeAPI) Validate() error {
	if a.Port < 0 {
		return errors.New("port must not be negative")
	}
	return nil
}

func (a *fakeAPI) Warnings() []string {
	if a.Complain {
		return []string{"this API is on a public address"}
	}
	return nil
}

func (*fakeAPI) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "fake", Description: "a fake API"}
}
func (*fakeAPI) NewAPIServer() (spec.APIServer, error) { return nil, nil }

func modulesWithAFakeAPI(t *testing.T) *registry.Registry {
	t.Helper()
	registered := modules(t)
	if err := registered.APIs().Register(func() spec.APIModule { return &fakeAPI{} }); err != nil {
		t.Fatal(err)
	}
	return registered
}

func documentWithAPIs(apis string) string {
	return strings.TrimSuffix(strings.TrimSpace(goodDocument), "}") + `, "apis": ` + apis + `}`
}

func parseAndCheck(t *testing.T, document string) (config.Diagnostics, []spec.APIModule) {
	t.Helper()
	configuration, diagnostics := config.Parse([]byte(document), modulesWithAFakeAPI(t))
	if configuration == nil {
		return diagnostics, nil
	}
	return append(diagnostics, config.Check(configuration)...), configuration.APIs
}

func TestAPIsAreDecodedLikeAnyModuleAndTwoOfOneTypeAreAllowed(t *testing.T) {
	diagnostics, apis := parseAndCheck(t, documentWithAPIs(`[
		{"type": "fake", "settings": {"port": 9090, "secret": "a"}},
		{"type": "fake", "settings": {"port": 9091, "secret": "b"}}
	]`))
	requireNoErrors(t, diagnostics)
	if len(apis) != 2 || apis[0].(*fakeAPI).Port != 9090 || apis[1].(*fakeAPI).Secret != "b" {
		t.Errorf("apis = %+v, want both entries decoded in order", apis)
	}
}

func TestNoAPIsIsFine(t *testing.T) {
	for _, document := range []string{goodDocument, documentWithAPIs(`[]`)} {
		diagnostics, apis := parseAndCheck(t, document)
		requireNoErrors(t, diagnostics)
		if len(apis) != 0 {
			t.Errorf("apis = %v, want none", apis)
		}
	}
}

func TestAnAPIThatCannotBeReadIsReportedWhereItIs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		apis    string
		path    string
		message string
	}{
		{"no type", `[{"settings": {}}]`, "apis[0]", `needs a "type" field`},
		{"an unknown type", `[{"type": "nowhere"}]`, "apis[0].type", "nowhere"},
		{"an unknown setting", `[{"type": "fake", "settings": {"colour": "red"}}]`, "apis[0].settings", `unknown field "colour"`},
		{"settings that are not an object", `[{"type": "fake", "settings": 5}]`, "apis[0].settings", "cannot unmarshal"},
		{"a setting its module refuses", `[{"type": "fake", "settings": {"port": 1}}, {"type": "fake", "settings": {"port": -1}}]`, "apis[1].settings", "must not be negative"},
		{"an unknown key beside the list", `[{"type": "fake", "color": "red"}]`, "", `unknown field "color"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostics, _ := parseAndCheck(t, documentWithAPIs(tc.apis))
			if !diagnostics.HasErrors() || !find(diagnostics, tc.path, tc.message) {
				t.Errorf("want an error at %q containing %q, got:\n%v", tc.path, tc.message, diagnostics)
			}
		})
	}
}

func TestAnAPIsWarningsCarryItsLocation(t *testing.T) {
	diagnostics, _ := parseAndCheck(t, documentWithAPIs(`[{"type": "fake", "settings": {"complain": true}}]`))
	requireNoErrors(t, diagnostics)
	if !find(diagnostics, "apis[0].settings", "public address") {
		t.Errorf("the module's warning is missing or misplaced:\n%v", diagnostics)
	}
}

func TestASelectorWithoutAnAPIIsWarnedAbout(t *testing.T) {
	selector := groupOutbound("proxy", "selector", `{"members": ["direct"]}`)
	withoutAPI := documentWithOutbounds(selector+","+directOutbound, "")
	diagnostics, failed := loadAndCheck(t, withoutAPI)
	if failed || !find(diagnostics, "outbounds[0]", "no apis are configured") {
		t.Errorf("want a warning for a selector with no API, got:\n%v", diagnostics)
	}

	withAPI := strings.TrimSuffix(strings.TrimSpace(withoutAPI), "}") + `, "apis": [{"type": "fake"}]}`
	diagnostics, _ = parseAndCheck(t, withAPI)
	if find(diagnostics, "outbounds[0]", "no apis are configured") {
		t.Errorf("a selector with an API was warned about:\n%v", diagnostics)
	}
}

func TestAnAPIWithLiveTrackingTurnedOffIsWarnedAbout(t *testing.T) {
	document := strings.TrimSuffix(documentWithAPIs(`[{"type": "fake"}]`), "}") + `, "stats": {"tracking": {"enabled": false}}}`
	diagnostics, _ := parseAndCheck(t, document)
	requireNoErrors(t, diagnostics)
	if !find(diagnostics, "stats.tracking.enabled", "cannot list or close") {
		t.Errorf("no warning that the API loses live conversations:\n%v", diagnostics)
	}

	diagnostics, _ = parseAndCheck(t, documentWithAPIs(`[{"type": "fake"}]`))
	if find(diagnostics, "stats.tracking.enabled", "cannot list or close") {
		t.Errorf("warned although live tracking is on by default with an API:\n%v", diagnostics)
	}
}
