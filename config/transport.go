package config

import (
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/spec"
)

func (b *configBuilder) buildStreamTransportStack(stack []rawModule, loc string) blueprint.StreamTransportStack {
	if len(stack) == 0 {
		return blueprint.StreamTransportStack{}
	}

	transports := b.modules.Transports()
	var built blueprint.StreamTransportStack

	baseLoc := indexLocation(loc, 0)
	if newModule, err := transports.LookupBaseStream(stack[0].Type); err != nil {
		b.diagnostics.report(baseLoc, err)
	} else {
		base := newModule()
		if b.decodeSettings(base, stack[0].Settings, fieldLocation(baseLoc, "settings")) {
			built.Base = base
		}
	}

	for index, layer := range stack[1:] {
		if overlay := b.buildStreamOverlay(layer, indexLocation(loc, index+1)); overlay != nil {
			built.Overlays = append(built.Overlays, overlay)
		}
	}

	return built
}

// Returns nil after reporting why, so the stack is built without it.
func (b *configBuilder) buildStreamOverlay(layer rawModule, layerLoc string) spec.OverlayStreamTransportModule {
	newModule, err := b.modules.Transports().LookupOverlayStream(layer.Type)
	if err != nil {
		b.diagnostics.report(layerLoc, err)
		return nil
	}
	overlay := newModule()
	if !b.decodeSettings(overlay, layer.Settings, fieldLocation(layerLoc, "settings")) {
		return nil
	}
	return overlay
}

func (b *configBuilder) buildDatagramTransportStack(stack []rawModule, loc string) blueprint.DatagramTransportStack {
	if len(stack) == 0 {
		return blueprint.DatagramTransportStack{}
	}

	transports := b.modules.Transports()
	var built blueprint.DatagramTransportStack

	baseLoc := indexLocation(loc, 0)
	if newModule, err := transports.LookupBaseDatagram(stack[0].Type); err != nil {
		b.diagnostics.report(baseLoc, err)
	} else {
		base := newModule()
		if b.decodeSettings(base, stack[0].Settings, fieldLocation(baseLoc, "settings")) {
			built.Base = base
		}
	}

	for index, layer := range stack[1:] {
		layerLoc := indexLocation(loc, index+1)

		newModule, err := transports.LookupOverlayDatagram(layer.Type)
		if err != nil {
			b.diagnostics.report(layerLoc, err)
			continue
		}
		overlay := newModule()
		if b.decodeSettings(overlay, layer.Settings, fieldLocation(layerLoc, "settings")) {
			built.Overlays = append(built.Overlays, overlay)
		}
	}

	return built
}
