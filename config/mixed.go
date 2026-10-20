package config

import (
	"bytes"
	"encoding/json"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/registry"
	"github.com/we-apranik/apranik/spec"
)

const (
	mixedProtocol = registry.MixedProtocolName

	mixedSOCKS5Module = "socks5"
	mixedHTTPModule   = "http"
)

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
	if err := types.CheckPositive(settings.FirstByteTimeout); err != nil {
		b.diagnostics.errorf(fieldLocation(loc, "first_byte_timeout"), "first_byte_timeout %v", err)
	}
	return &blueprint.Mixed{SOCKS5: socks5, HTTP: http, Users: settings.Users, FirstByteTimeout: durationOrNil(settings.FirstByteTimeout)}
}

func (b *configBuilder) buildMixedHalf(moduleName string, raw json.RawMessage, loc string) spec.StreamProxyModule {
	newModules, err := b.modules.ProxyProtocols().Lookup(moduleName, registry.KindStreamProxy)
	if err != nil {
		b.diagnostics.report(loc, err)
		return nil
	}
	half := newModules()
	if !b.decodeSettings(half.Config, raw, loc) {
		return nil
	}
	return half.Stream
}
