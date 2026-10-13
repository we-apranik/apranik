package blueprint_test

import (
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
)

func pointerTo[T any](value T) *T { return &value }

func xudp(enabled *bool) *blueprint.MuxCoolXUDP { return &blueprint.MuxCoolXUDP{Enabled: enabled} }

// Left out means the other cores' default: UDP as XUDP.
func TestMuxCoolDefaultsToXUDP(t *testing.T) {
	for name, muxCool := range map[string]*blueprint.MuxCool{
		"nil":            nil,
		"empty":          {},
		"tcp only":       {TCP: &blueprint.MuxCoolTCP{Enabled: pointerTo(false)}},
		"udp empty":      {UDP: &blueprint.MuxCoolUDP{}},
		"udp on":         {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(true)}},
		"udp on, xudp":   {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(true), XUDP: xudp(pointerTo(true))}},
		"xudp only true": {UDP: &blueprint.MuxCoolUDP{XUDP: xudp(pointerTo(true))}},
		"xudp empty":     {UDP: &blueprint.MuxCoolUDP{XUDP: xudp(nil)}},
	} {
		if !muxCool.Options().XUDP {
			t.Errorf("%s: XUDP = false, want the default true", name)
		}
	}
}

func TestMuxCoolUDPOffOrNotXUDPSendsPlainUDP(t *testing.T) {
	for name, muxCool := range map[string]*blueprint.MuxCool{
		"udp off":      {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(false)}},
		"xudp false":   {UDP: &blueprint.MuxCoolUDP{XUDP: xudp(pointerTo(false))}},
		"off and xudp": {UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(false), XUDP: xudp(pointerTo(true))}},
	} {
		if muxCool.Options().XUDP {
			t.Errorf("%s: XUDP = true, want plain UDP", name)
		}
	}
}

func globalIDOf(globalID *blueprint.MuxCoolGlobalID) *blueprint.MuxCool {
	return &blueprint.MuxCool{UDP: &blueprint.MuxCoolUDP{XUDP: &blueprint.MuxCoolXUDP{GlobalID: globalID}}}
}

func TestTheGlobalIDIsOnByDefaultAndOffWhenTurnedOffOrWhenXUDPIsOff(t *testing.T) {
	for name, test := range map[string]struct {
		muxCool *blueprint.MuxCool
		want    bool
	}{
		"nil":                   {nil, true},
		"empty":                 {&blueprint.MuxCool{}, true},
		"global_id empty":       {globalIDOf(&blueprint.MuxCoolGlobalID{}), true},
		"global_id on":          {globalIDOf(&blueprint.MuxCoolGlobalID{Enabled: pointerTo(true)}), true},
		"global_id off":         {globalIDOf(&blueprint.MuxCoolGlobalID{Enabled: pointerTo(false)}), false},
		"udp off":               {&blueprint.MuxCool{UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(false)}}, false},
		"xudp off":              {&blueprint.MuxCool{UDP: &blueprint.MuxCoolUDP{XUDP: xudp(pointerTo(false))}}, false},
		"hold_time only":        {globalIDOf(&blueprint.MuxCoolGlobalID{HoldTime: pointerTo(time.Second)}), true},
		"global_id on, udp off": {&blueprint.MuxCool{UDP: &blueprint.MuxCoolUDP{Enabled: pointerTo(false), XUDP: &blueprint.MuxCoolXUDP{GlobalID: &blueprint.MuxCoolGlobalID{Enabled: pointerTo(true)}}}}, false},
	} {
		if got := test.muxCool.XUDPGlobalIDEnabled(); got != test.want {
			t.Errorf("%s: XUDPGlobalIDEnabled = %v, want %v", name, got, test.want)
		}
	}
}

func TestTheHoldTimeIsZeroWhenLeftOutSoTheConsumerAppliesItsDefault(t *testing.T) {
	if got := (*blueprint.MuxCool)(nil).XUDPGlobalIDHoldTime(); got != 0 {
		t.Errorf("nil: %s, want 0", got)
	}
	if got := globalIDOf(&blueprint.MuxCoolGlobalID{}).XUDPGlobalIDHoldTime(); got != 0 {
		t.Errorf("left out: %s, want 0", got)
	}
	if got := globalIDOf(&blueprint.MuxCoolGlobalID{HoldTime: pointerTo(90 * time.Second)}).XUDPGlobalIDHoldTime(); got != 90*time.Second {
		t.Errorf("written: %s, want 90s", got)
	}
}
