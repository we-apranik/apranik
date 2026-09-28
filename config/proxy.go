package config

import "github.com/The-Apranik/Apranik/spec"

// Nil on any failure: an inbound with no usable proxy is caught by Check,
// which is where "this inbound has no proxy protocol" belongs.
func (b *configBuilder) buildStreamProxy(proxy rawModule, loc string) spec.StreamProxyModule {
	if proxy.Type == "" {
		b.diagnostics.report(loc, errMissingField("type", "protocol"))
		return nil
	}

	newModule, err := b.modules.ProxyProtocols().LookupStream(proxy.Type)
	if err != nil {
		b.diagnostics.report(loc, err)
		return nil
	}

	built := newModule()
	if !b.decodeSettings(built, proxy.Settings, fieldLocation(loc, "settings")) {
		return nil
	}
	return built
}

// Nil when the protocol has no datagram-over-stream kind, which is not an
// error: that protocol simply carries no UDP inside its streams. Called only
// after buildStreamProxy accepted the same settings, so decoding them again
// reports nothing new.
func (b *configBuilder) buildDatagramOverStreamProxy(proxy rawModule, loc string) spec.DatagramOverStreamProxyModule {
	newModule, err := b.modules.ProxyProtocols().LookupDatagramOverStream(proxy.Type)
	if err != nil {
		return nil
	}

	built := newModule()
	if !b.decodeSettings(built, proxy.Settings, fieldLocation(loc, "settings")) {
		return nil
	}
	return built
}

func (b *configBuilder) buildDatagramProxy(proxy rawModule, loc string) spec.DatagramProxyModule {
	if proxy.Type == "" {
		b.diagnostics.report(loc, errMissingField("type", "protocol"))
		return nil
	}

	newModule, err := b.modules.ProxyProtocols().LookupDatagram(proxy.Type)
	if err != nil {
		b.diagnostics.report(loc, err)
		return nil
	}

	built := newModule()
	if !b.decodeSettings(built, proxy.Settings, fieldLocation(loc, "settings")) {
		return nil
	}
	return built
}
