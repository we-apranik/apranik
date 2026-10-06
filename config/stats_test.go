package config_test

import (
	"strings"
	"testing"

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
	document := documentWithStats(`{"enabled": true, "inbounds": ["socks-in"], "outbounds": ["freedom"], "users": ["alice"]}`)
	configuration, _ := config.Parse([]byte(document), modules(t))
	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("unexpected errors:\n%v", diagnostics.Err())
	}

	section := configuration.Stats
	if section == nil || section.Disabled || len(section.Inbounds) != 1 || len(section.Outbounds) != 1 || len(section.Users) != 1 {
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

func TestAnEmptyListedNameIsRefused(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, documentWithStats(`{"users": ["alice", ""]}`))
	if !failed || !find(diagnostics, "stats.users[1]", "needs a name") {
		t.Errorf("an empty user name was not refused:\n%v", diagnostics)
	}
}

func TestAUserNameIsNotCheckedAgainstAnything(t *testing.T) {
	_, failed := loadAndCheck(t, documentWithStats(`{"users": ["nobody-configured-this"]}`))
	if failed {
		t.Error("a user name was refused, though core cannot know the users")
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

func TestTheLiveConnectionsAndDialHealthSwitchesAreRead(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(documentWithStats(`{"live_connections": false, "dial_health": true}`)), modules(t))
	requireNoErrors(t, diagnostics)

	section := configuration.Stats
	if section == nil || section.LiveConnections == nil || *section.LiveConnections || !section.DialHealth {
		t.Errorf("Stats = %+v, want live_connections false and dial_health true", section)
	}
}

func TestTheSwitchesAreUnsetUntilTheConfigSaysSo(t *testing.T) {
	configuration, _ := config.Parse([]byte(documentWithStats(`{}`)), modules(t))
	if configuration.Stats.LiveConnections != nil || configuration.Stats.DialHealth {
		t.Errorf("Stats = %+v, want live_connections unset (the default) and dial_health off", configuration.Stats)
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
