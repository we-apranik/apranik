package config

import "github.com/The-Apranik/Apranik/core/registry"

// A protocol is looked up by name, built and decoded once, whatever kinds it
// fills. The wanted kind is the one this position needs, so a protocol that
// lacks it is told apart from an unknown name. The zero value on any failure:
// an inbound with no usable proxy is caught by Check, which is where "this
// inbound has no proxy protocol" belongs.
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
