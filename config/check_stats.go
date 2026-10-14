package config

import (
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/net/udp"
)

// A tag a list names must exist, or the user would count nothing and never
// know why. User names cannot be checked: core learns them only when a
// protocol authenticates one.
func checkStats(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	section := coreBlueprint.Stats
	warnAboutTrackingSettingsThatDoNothing(diagnostics, coreBlueprint)
	if section == nil {
		return
	}

	if len(coreBlueprint.APIs) > 0 && !section.Disabled && section.Tracking.Enabled != nil && !*section.Tracking.Enabled {
		diagnostics.warnf(fieldLocation("stats", "tracking", "enabled"),
			"is false, so an API cannot list or close live conversations or show online users")
	}

	namesListed := len(section.Inbounds)+len(section.Outbounds) > 0
	if section.Disabled && namesListed {
		diagnostics.warnf("stats", "counting is disabled, so the lists of inbounds and outbounds are ignored")
		return
	}
	if section.Disabled && (section.Tracking != (blueprint.StatsTracking{}) || section.DialHealth) {
		diagnostics.warnf("stats", "counting is disabled, so tracking and dial_health are ignored")
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
	checkTracking(diagnostics, coreBlueprint)
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

// The UDP settings of stats.tracking are read only while live conversations are
// tracked: when stats.tracking.enabled is true, or absent with an API
// configured.
func warnAboutTrackingSettingsThatDoNothing(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	if coreBlueprint.Stats == nil || coreBlueprint.IsLiveTrackingEnabled() {
		return
	}
	tracking := coreBlueprint.Stats.Tracking
	if tracking.UDPEntryIdleTimeout != nil {
		diagnostics.warnf(fieldLocation("stats", "tracking", "udp_entry_idle_timeout"),
			"does nothing: live conversations are tracked only when stats.tracking.enabled is true, or absent while an API is configured")
	}
	if tracking.UDPMaxEntriesPerSession != nil {
		diagnostics.warnf(fieldLocation("stats", "tracking", "udp_max_entries_per_session"),
			"does nothing: live conversations are tracked only when stats.tracking.enabled is true, or absent while an API is configured")
	}
}

// A written value must be positive: zero never stands for the default.
func checkTracking(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	tracking := coreBlueprint.Stats.Tracking
	idleLocation := fieldLocation("stats", "tracking", "udp_entry_idle_timeout")
	if tracking.UDPEntryIdleTimeout != nil {
		if *tracking.UDPEntryIdleTimeout <= 0 {
			diagnostics.errorf(idleLocation,
				"must be greater than zero (leave it out for the default, %s): an entry silent for this long leaves the list, and a UDP session has no other way to end one",
				udp.DefaultLiveTrackingUDPEntryIdleTimeout)
		} else {
			for index, inbound := range coreBlueprint.Inbounds {
				sessionIdle := effectiveUDPSessionIdleTimeout(inbound)
				if *tracking.UDPEntryIdleTimeout <= sessionIdle {
					continue
				}
				diagnostics.warnf(idleLocation, "%s is longer than the udp_session_idle_timeout of inbounds[%d] (%s): its session ends first, so this setting never takes effect there",
					*tracking.UDPEntryIdleTimeout, index, sessionIdle)
			}
		}
	}
	checkUDPMaxEntriesPerSession(diagnostics, fieldLocation("stats", "tracking", "udp_max_entries_per_session"),
		tracking.UDPMaxEntriesPerSession, udp.DefaultLiveTrackingUDPMaxEntriesPerSession)
}
