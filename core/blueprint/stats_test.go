package blueprint

import (
	"testing"

	"github.com/The-Apranik/Apranik/spec"
)

func TestLiveTrackingIsEnabledByDefaultOnExactlyWhenAnAPIIsConfigured(t *testing.T) {
	on, off := true, false
	oneAPI := []spec.APIModule{nil}
	for _, tc := range []struct {
		name      string
		blueprint Blueprint
		want      bool
	}{
		{"no stats section, no API", Blueprint{}, false},
		{"no stats section, an API", Blueprint{APIs: oneAPI}, true},
		{"unset, an API", Blueprint{Stats: &Stats{}, APIs: oneAPI}, true},
		{"unset, no API", Blueprint{Stats: &Stats{}}, false},
		{"true, no API", Blueprint{Stats: &Stats{Tracking: StatsTracking{Enabled: &on}}}, true},
		{"false beats an API", Blueprint{Stats: &Stats{Tracking: StatsTracking{Enabled: &off}}, APIs: oneAPI}, false},
		{"counting disabled beats everything", Blueprint{Stats: &Stats{Disabled: true, Tracking: StatsTracking{Enabled: &on}}, APIs: oneAPI}, false},
	} {
		if got := tc.blueprint.IsLiveTrackingEnabled(); got != tc.want {
			t.Errorf("%s: IsLiveTrackingEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
