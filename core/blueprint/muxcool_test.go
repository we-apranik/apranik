package blueprint_test

import (
	"testing"

	"github.com/The-Apranik/Apranik/core/blueprint"
)

func pointerTo[T any](value T) *T { return &value }

// Left out means the other cores' default: UDP as XUDP.
func TestMuxCoolDefaultsToXUDP(t *testing.T) {
	for name, muxCool := range map[string]*blueprint.MuxCool{
		"nil":            nil,
		"empty":          {},
		"tcp only":       {TCP: &blueprint.MuxCoolTCP{Enabled: pointerTo(false)}},
		"udp empty":      {UDP: &blueprint.MuxCoolUDP{}},
		"udp on":         {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(true)}},
		"udp on, xudp":   {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(true), XUDP: pointerTo(true)}},
		"xudp only true": {UDP: &blueprint.MuxCoolUDP{XUDP: pointerTo(true)}},
	} {
		if !muxCool.Options().XUDP {
			t.Errorf("%s: XUDP = false, want the default true", name)
		}
	}
}

func TestMuxCoolUDPOffOrNotXUDPSendsPlainUDP(t *testing.T) {
	for name, muxCool := range map[string]*blueprint.MuxCool{
		"udp off":      {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(false)}},
		"xudp false":   {UDP: &blueprint.MuxCoolUDP{XUDP: pointerTo(false)}},
		"off and xudp": {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(false), XUDP: pointerTo(true)}},
	} {
		if muxCool.Options().XUDP {
			t.Errorf("%s: XUDP = true, want plain UDP", name)
		}
	}
}
