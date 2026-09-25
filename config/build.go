// Package config turns a JSON config file into a blueprint.Blueprint. A user on a
// censored network needs every mistake reported before anything runs, so it
// decodes and checks the whole config file and reports each problem with its
// location instead of stopping at the first.
package config

import (
	"bytes"
	"encoding/json"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/registry"
)

// Parse turns one config file into a *blueprint.Blueprint without checking it. The
// config is partial when diagnostics.HasErrors is true. LoadFile and LoadURL
// are Parse plus Check; use Parse alone only to run Check yourself.
func Parse(data []byte, modules *registry.Registry) (*blueprint.Blueprint, Diagnostics) {
	builder := &configBuilder{modules: modules, strict: true}
	return builder.build(data)
}

// configBuilder reports what cannot be read: syntax, unknown names, values
// that do not parse. What can be read but cannot be run -- a missing field, a
// conflicting pair, an unsafe value -- is Check's, so it holds for a
// blueprint.Blueprint however it was made.
type configBuilder struct {
	modules     *registry.Registry
	strict      bool
	diagnostics Diagnostics
}

func (b *configBuilder) build(data []byte) (*blueprint.Blueprint, Diagnostics) {
	// The envelope alone, tolerantly, so version and min_core are answered
	// before any field this core may not recognise is parsed.
	var envelope envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		b.diagnostics.report("", err)
		return nil, b.diagnostics
	}

	if !b.checkVersions(envelope) {
		return nil, b.diagnostics
	}
	if envelope.Strict != nil {
		b.strict = *envelope.Strict
	}

	var raw rawConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	if b.strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&raw); err != nil {
		b.diagnostics.report("", err)
		return nil, b.diagnostics
	}

	coreBlueprint := &blueprint.Blueprint{
		Version: raw.Version,
		Log:     b.buildLog(raw.Log, "log"),
	}

	for index, inbound := range raw.Inbounds {
		coreBlueprint.Inbounds = append(coreBlueprint.Inbounds,
			b.buildInbound(inbound, indexLocation("inbounds", index)))
	}
	for index, outbound := range raw.Outbounds {
		coreBlueprint.Outbounds = append(coreBlueprint.Outbounds,
			b.buildOutbound(outbound, indexLocation("outbounds", index)))
	}

	return coreBlueprint, b.diagnostics
}
