package config

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	mixedProtocol = registry.MixedProtocolName

	mixedSOCKS5Module = "socks5"
	mixedHTTPModule   = "http"
)

// Nil when either half fails, so Check reports the inbound as having no
// protocol only after the real cause was reported here.
func (b *configBuilder) buildMixed(raw json.RawMessage, loc string) *blueprint.Mixed {
	var settings rawMixedSettings
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if b.strict {
			decoder.DisallowUnknownFields()
		}
		if err := decoder.Decode(&settings); err != nil {
			b.diagnostics.report(loc, err)
			return nil
		}
	}

	socks5 := b.buildMixedHalf(mixedSOCKS5Module, settings.SOCKS5, fieldLocation(loc, mixedSOCKS5Module))
	http := b.buildMixedHalf(mixedHTTPModule, settings.HTTP, fieldLocation(loc, mixedHTTPModule))
	if socks5 == nil || http == nil {
		return nil
	}
	if settings.FirstByteTimeout < 0 {
		b.diagnostics.errorf(fieldLocation(loc, "first_byte_timeout"), "first_byte_timeout must not be negative")
	}
	return &blueprint.Mixed{SOCKS5: socks5, HTTP: http, FirstByteTimeout: time.Duration(settings.FirstByteTimeout)}
}

// An absent key leaves the module's own defaults, as for freedom's halves.
func (b *configBuilder) buildMixedHalf(moduleName string, raw json.RawMessage, loc string) spec.StreamProxyModule {
	newModule, err := b.modules.ProxyProtocols().LookupStream(moduleName)
	if err != nil {
		b.diagnostics.report(loc, err)
		return nil
	}
	half := newModule()
	if !b.decodeSettings(half, raw, loc) {
		return nil
	}
	return half
}
