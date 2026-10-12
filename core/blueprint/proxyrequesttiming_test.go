package blueprint_test

import (
	"slices"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/spec"
)

func TestProxyRequestTimingDefaultsToFourHundredMillisecondsAndNoSkippedPort(t *testing.T) {
	for name, timing := range map[string]*blueprint.ProxyRequestTiming{
		"nil":                    nil,
		"empty":                  {},
		"skip empty":             {Skip: &blueprint.ProxyRequestTimingSkip{}},
		"skip off":               {Skip: &blueprint.ProxyRequestTimingSkip{Enabled: pointerTo(false)}},
		"skip off with its list": {Skip: &blueprint.ProxyRequestTimingSkip{Enabled: pointerTo(false), Ports: []int{25}}},
	} {
		options := timing.Options()
		if options.MaxDelay != 400*time.Millisecond || len(options.SkipPorts) != 0 {
			t.Errorf("%s: options = %+v, want 400ms and no port", name, options)
		}
	}
}

func TestProxyRequestTimingTakesTheWrittenMaximumDelay(t *testing.T) {
	timing := &blueprint.ProxyRequestTiming{MaxDelay: pointerTo(30 * time.Millisecond)}
	if got := timing.Options().MaxDelay; got != 30*time.Millisecond {
		t.Errorf("MaxDelay = %v, want 30ms", got)
	}
}

func TestSkipOnWithNoPortsUsesTheBuiltInList(t *testing.T) {
	timing := &blueprint.ProxyRequestTiming{Skip: &blueprint.ProxyRequestTimingSkip{Enabled: pointerTo(true)}}
	want := []uint16{21, 25, 110, 143, 587, 3306}
	if got := timing.Options().SkipPorts; !slices.Equal(got, want) {
		t.Errorf("SkipPorts = %v, want %v", got, want)
	}
}

func TestSkipOnWithPortsReplacesTheBuiltInList(t *testing.T) {
	timing := &blueprint.ProxyRequestTiming{Skip: &blueprint.ProxyRequestTimingSkip{Enabled: pointerTo(true), Ports: []int{22, 6000}}}
	want := spec.ProxyRequestTimingOptions{MaxDelay: 400 * time.Millisecond, SkipPorts: []uint16{22, 6000}}
	got := timing.Options()
	if got.MaxDelay != want.MaxDelay || !slices.Equal(got.SkipPorts, want.SkipPorts) {
		t.Errorf("options = %+v, want %+v", got, want)
	}
}
