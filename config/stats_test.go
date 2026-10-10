package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config"
)

func documentWithStats(section string) string {
	return strings.TrimSuffix(strings.TrimSpace(goodDocument), "}") + `, "stats": ` + section + `}`
}

func TestWithoutAStatsSectionEverythingIsCounted(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(goodDocument), modules(t))
	requireNoErrors(t, diagnostics)

	if configuration.Stats != nil {
		t.Errorf("Stats = %+v, want nil: nil counts everything", configuration.Stats)
	}
}

func TestAStatsSectionReadsItsSwitchAndLists(t *testing.T) {
	document := documentWithStats(`{"enabled": true, "inbounds": ["socks-in"], "outbounds": ["freedom"]}`)
	configuration, _ := config.Parse([]byte(document), modules(t))
	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("unexpected errors:\n%v", diagnostics.Err())
	}

	section := configuration.Stats
	if section == nil || section.Disabled || len(section.Inbounds) != 1 || len(section.Outbounds) != 1 {
		t.Errorf("Stats = %+v", section)
	}
}

func TestAnEmptyStatsSectionStillCountsEverything(t *testing.T) {
	configuration, _ := config.Parse([]byte(documentWithStats(`{}`)), modules(t))
	if configuration.Stats == nil || configuration.Stats.Disabled {
		t.Errorf("Stats = %+v, want present and enabled", configuration.Stats)
	}
}

func TestStatsCanBeSwitchedOff(t *testing.T) {
	configuration, _ := config.Parse([]byte(documentWithStats(`{"enabled": false}`)), modules(t))
	if configuration.Stats == nil || !configuration.Stats.Disabled {
		t.Errorf("Stats = %+v, want disabled", configuration.Stats)
	}
}

func TestAStatsListNamingAMissingInboundOrOutboundIsRefused(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithStats(`{"inbounds": ["nope"], "outbounds": ["freedom", "gone"]}`))
	if !failed {
		t.Fatal("a stats list naming a missing tag was accepted")
	}
	if !find(diagnostics, "stats.inbounds[0]", `no inbound is tagged "nope"`) {
		t.Errorf("the missing inbound was not reported:\n%v", diagnostics)
	}
	if !find(diagnostics, "stats.outbounds[1]", `no outbound is tagged "gone"`) {
		t.Errorf("the missing outbound was not reported:\n%v", diagnostics)
	}
}

func TestListsInADisabledStatsSectionAreWarnedAbout(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithStats(`{"enabled": false, "inbounds": ["nope"]}`))
	if failed {
		t.Fatalf("a disabled section's lists were checked:\n%v", diagnostics.Err())
	}
	if !find(diagnostics, "stats", "ignored") {
		t.Errorf("no warning that the lists are ignored:\n%v", diagnostics)
	}
}

func TestAListedNameRepeatedIsWarnedAbout(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithStats(`{"inbounds": ["socks-in", "socks-in"]}`))
	if failed || !find(diagnostics, "stats.inbounds[1]", "already listed") {
		t.Errorf("a repeated name was not warned about:\n%v", diagnostics)
	}
}

func TestAnUnknownStatsKeyIsRefusedInStrictMode(t *testing.T) {
	_, failed := loadAndCheck(t, documentWithStats(`{"reset": true}`))
	if !failed {
		t.Error("an unknown stats key was accepted")
	}
}

func TestTheTrackingObjectAndDialHealthAreRead(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(documentWithStats(
		`{"tracking": {"enabled": false, "udp_entry_idle_timeout": "30s", "udp_max_entries_per_session": 50}, "dial_health": true}`)), modules(t))
	requireNoErrors(t, diagnostics)

	section := configuration.Stats
	if section == nil || !section.DialHealth || section.Tracking.Enabled == nil || *section.Tracking.Enabled ||
		section.Tracking.UDPEntryIdleTimeout == nil || *section.Tracking.UDPEntryIdleTimeout != 30*time.Second ||
		section.Tracking.UDPMaxEntriesPerSession == nil || *section.Tracking.UDPMaxEntriesPerSession != 50 {
		t.Errorf("Stats = %+v, want tracking off with 30s and 50, and dial_health true", section)
	}
}

func TestTheSwitchesAreUnsetUntilTheConfigSaysSo(t *testing.T) {
	configuration, _ := config.Parse([]byte(documentWithStats(`{}`)), modules(t))
	tracking := configuration.Stats.Tracking
	if tracking.Enabled != nil || tracking.UDPEntryIdleTimeout != nil || tracking.UDPMaxEntriesPerSession != nil || configuration.Stats.DialHealth {
		t.Errorf("Stats = %+v, want tracking absent (the defaults) and dial_health off", configuration.Stats)
	}
}

func TestZeroAndNegativeTrackingSettingsAreRefused(t *testing.T) {
	for key, values := range map[string][]string{
		"udp_entry_idle_timeout":      {`"0s"`, `"-1s"`},
		"udp_max_entries_per_session": {`0`, `-1`},
	} {
		for _, value := range values {
			diagnostics, failed := loadAndCheck(t, documentWithStats(`{"tracking": {"`+key+`": `+value+`}}`))
			if !failed || !find(diagnostics, "stats.tracking."+key, "must be greater than zero") {
				t.Errorf("%s = %s was not refused with its path: %v", key, value, diagnostics)
			}
		}
	}
}

func TestTrackingUDPSettingsThatDoNothingAreWarnedAbout(t *testing.T) {
	const settings = `"udp_entry_idle_timeout": "30s", "udp_max_entries_per_session": 50`
	for name, tracking := range map[string]string{
		"no API, tracking absent": `{"tracking": {` + settings + `}}`,
		"tracking turned off":     `{"tracking": {"enabled": false, ` + settings + `}}`,
	} {
		diagnostics, failed := loadAndCheck(t, documentWithStats(tracking))
		if failed || !find(diagnostics, "stats.tracking.udp_entry_idle_timeout", "does nothing") ||
			!find(diagnostics, "stats.tracking.udp_max_entries_per_session", "does nothing") {
			t.Errorf("%s: settings that do nothing were not warned about (failed=%v): %v", name, failed, diagnostics)
		}
	}

	diagnostics, _ := loadAndCheck(t, documentWithStats(`{"tracking": {"enabled": true, `+settings+`}}`))
	if find(diagnostics, "stats.tracking.udp_entry_idle_timeout", "does nothing") {
		t.Errorf("warned that the settings do nothing, though tracking is on: %v", diagnostics)
	}
}

func TestATrackingEntryIdleTimeoutLongerThanASessionsIsWarnedAbout(t *testing.T) {
	diagnostics, _ := loadAndCheck(t, documentWithStats(`{"tracking": {"enabled": true, "udp_entry_idle_timeout": "10m"}}`))
	if !find(diagnostics, "stats.tracking.udp_entry_idle_timeout", "longer than the udp_session_idle_timeout of inbounds[0]") {
		t.Errorf("10m against the default 5m session idle time was not warned about: %v", diagnostics)
	}
}

func TestTheSwitchesInADisabledStatsSectionAreWarnedAbout(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithStats(`{"enabled": false, "dial_health": true}`))
	if failed {
		t.Fatalf("a disabled section's switches were refused:\n%v", diagnostics.Err())
	}
	if !find(diagnostics, "stats", "ignored") {
		t.Errorf("no warning that the switches are ignored:\n%v", diagnostics)
	}
}
