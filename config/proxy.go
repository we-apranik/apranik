package config

import "github.com/we-apranik/apranik/core/registry"

func (b *configBuilder) buildProxyModules(proxy rawModule, loc string, wanted registry.Kind) registry.ProxyModules {
	if proxy.Type == "" {
		b.diagnostics.report(loc, errMissingField("type", "protocol"))
		return registry.ProxyModules{}
	}

	newModules, err := b.modules.ProxyProtocols().Lookup(proxy.Type, wanted)
	if err != nil {
		b.diagnostics.report(loc, err)
		return registry.ProxyModules{}
	}

	modules := newModules()
	if !b.decodeSettings(modules.Config, proxy.Settings, fieldLocation(loc, "settings")) {
		return registry.ProxyModules{}
	}
	return modules
}
