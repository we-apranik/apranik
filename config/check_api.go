package config

import (
	"github.com/we-apranik/apranik/core/blueprint"
)

// What an API's own settings can say is its module's: Validate and Warnings.
// Nothing here listens, so a port that is taken shows when the API starts.
func checkAPIs(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	for index, api := range coreBlueprint.APIs {
		settingsLoc := fieldLocation(indexLocation("apis", index), "settings")
		if err := api.Validate(); err != nil {
			diagnostics.report(settingsLoc, err)
			continue
		}
		collectWarnings(diagnostics, api, settingsLoc)
	}

	if len(coreBlueprint.APIs) > 0 {
		return
	}
	for index, outbound := range coreBlueprint.Outbounds {
		if outbound.Group != nil && outbound.Group.Type == blueprint.GroupTypeSelector {
			diagnostics.warnf(indexLocation("outbounds", index),
				"selector %q can only be changed through an API, and no apis are configured, so it will stay on its first member", outbound.Tag)
		}
	}
}
