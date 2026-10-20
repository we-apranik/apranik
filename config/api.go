package config

import (
	"github.com/we-apranik/apranik/spec"
)

func (b *configBuilder) buildAPIs(raw []rawModule, loc string) []spec.APIModule {
	var apis []spec.APIModule
	for index, rawAPI := range raw {
		apiLoc := indexLocation(loc, index)
		if rawAPI.Type == "" {
			b.diagnostics.report(apiLoc, errMissingField("type", "API"))
			continue
		}
		newModule, err := b.modules.APIs().Lookup(rawAPI.Type)
		if err != nil {
			b.diagnostics.report(fieldLocation(apiLoc, "type"), err)
			continue
		}
		api := newModule()
		if b.decodeSettings(api, rawAPI.Settings, fieldLocation(apiLoc, "settings")) {
			apis = append(apis, api)
		}
	}
	return apis
}
