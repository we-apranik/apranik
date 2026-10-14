// Package config turns a JSON config file into a blueprint.Blueprint. A user on a
// censored network needs every mistake reported before anything runs, so it
// decodes and checks the whole config file and reports each problem with its
// location instead of stopping at the first.
package config

import (
	"bytes"
	"encoding/json"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/registry"
)

// Parse turns one config file into a *blueprint.Blueprint without checking it. The
// config is partial when diagnostics.HasErrors is true. LoadFile and LoadURL
// are Parse plus Check; use Parse alone only to run Check yourself.
func Parse(data []byte, modules *registry.Registry, options ...ParseOption) (*blueprint.Blueprint, Diagnostics) {
	builder := &configBuilder{modules: modules, strict: true}
	for _, option := range options {
		option(builder)
	}
	return builder.build(data)
}

type ParseOption func(*configBuilder)

// WithBaseDirectory is what relative paths in the config file, such as a
// dataset's file, are relative to. Without it they stay relative, and are
// read from the working directory.
func WithBaseDirectory(directory string) ParseOption {
	return func(b *configBuilder) { b.baseDirectory = directory }
}

// configBuilder reports what cannot be read: syntax, unknown names, values
// that do not parse. What can be read but cannot be run -- a missing field, a
// conflicting pair, an unsafe value -- is Check's, so it holds for a
// blueprint.Blueprint however it was made.
type configBuilder struct {
	modules       *registry.Registry
	strict        bool
	baseDirectory string
	diagnostics   Diagnostics
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
	for index, dataset := range raw.Datasets {
		coreBlueprint.Datasets = append(coreBlueprint.Datasets, b.buildDataset(dataset, indexLocation("datasets", index)))
	}
	if raw.DNS != nil {
		coreBlueprint.DNS = b.buildDNS(*raw.DNS, "dns")
	}
	if raw.Routing != nil {
		coreBlueprint.Routing = b.buildRouting(*raw.Routing, "routing")
	}
	if raw.Stats != nil {
		coreBlueprint.Stats = &blueprint.Stats{
			Disabled:  raw.Stats.Enabled != nil && !*raw.Stats.Enabled,
			Inbounds:  raw.Stats.Inbounds,
			Outbounds: raw.Stats.Outbounds,

			DialHealth: raw.Stats.DialHealth,
		}
		if tracking := raw.Stats.Tracking; tracking != nil {
			coreBlueprint.Stats.Tracking = blueprint.StatsTracking{
				Enabled:                 tracking.Enabled,
				UDPEntryIdleTimeout:     durationOrNil(tracking.UDPEntryIdleTimeout),
				UDPMaxEntriesPerSession: tracking.UDPMaxEntriesPerSession,
			}
		}
	}

	coreBlueprint.APIs = b.buildAPIs(raw.APIs, "apis")

	return coreBlueprint, b.diagnostics
}
