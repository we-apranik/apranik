package config

import (
	"github.com/The-Apranik/Apranik/core/blueprint"
)

// A tag a list names must exist, or the user would count nothing and never
// know why. User names cannot be checked: core learns them only when a
// protocol authenticates one.
func checkStats(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	section := coreBlueprint.Stats
	warnAboutLiveConnectionSettingsThatDoNothing(diagnostics, coreBlueprint)
	if section == nil {
		return
	}

	namesListed := len(section.Inbounds)+len(section.Outbounds)+len(section.Users) > 0
	if section.Disabled && namesListed {
		diagnostics.warnf("stats", "counting is disabled, so the lists of inbounds, outbounds and users are ignored")
		return
	}
	if section.Disabled && (section.LiveConnections != nil || section.DialHealth) {
		diagnostics.warnf("stats", "counting is disabled, so live_connections and dial_health are ignored")
		return
	}

	inboundTags := make(map[string]bool, len(coreBlueprint.Inbounds))
	for _, inbound := range coreBlueprint.Inbounds {
		inboundTags[inbound.Tag] = true
	}
	outboundTags := make(map[string]bool, len(coreBlueprint.Outbounds))
	for _, outbound := range coreBlueprint.Outbounds {
		outboundTags[outbound.Tag] = true
	}

	checkStatsNames(diagnostics, section.Inbounds, inboundTags, "inbound", fieldLocation("stats", "inbounds"))
	checkStatsNames(diagnostics, section.Outbounds, outboundTags, "outbound", fieldLocation("stats", "outbounds"))
	checkStatsNames(diagnostics, section.Users, nil, "user", fieldLocation("stats", "users"))
}

// existing is nil when the names cannot be looked up.
func checkStatsNames(diagnostics *Diagnostics, names []string, existing map[string]bool, noun, loc string) {
	seen := make(map[string]int, len(names))
	for position, name := range names {
		nameLoc := indexLocation(loc, position)
		switch first, repeated := seen[name]; {
		case name == "":
			diagnostics.errorf(nameLoc, "a listed %s needs a name", noun)
			continue
		case repeated:
			diagnostics.warnf(nameLoc, "%q is already listed at index %d", name, first)
			continue
		case existing != nil && !existing[name]:
			diagnostics.errorf(nameLoc, "no %s is tagged %q", noun, name)
		}
		seen[name] = position
	}
}

// An inbound's UDP live connection settings are read only while live
// connections are listed, which is off unless the stats section turns it on.
func warnAboutLiveConnectionSettingsThatDoNothing(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	section := coreBlueprint.Stats
	if section != nil && !section.Disabled && section.LiveConnections != nil && *section.LiveConnections {
		return
	}
	for index, inbound := range coreBlueprint.Inbounds {
		inboundLocation := indexLocation("inbounds", index)
		if inbound.UDPSessionLiveConnectionsIdleTimeout != 0 {
			diagnostics.warnf(fieldLocation(inboundLocation, "udp_session_live_connections_idle_timeout"),
				"does nothing: live connections are listed only when stats.live_connections is true")
		}
		if inbound.UDPSessionLiveConnectionsMaxEntries != 0 {
			diagnostics.warnf(fieldLocation(inboundLocation, "udp_session_live_connections_max_entries"),
				"does nothing: live connections are listed only when stats.live_connections is true")
		}
	}
}
