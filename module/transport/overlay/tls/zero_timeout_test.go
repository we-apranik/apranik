package tls

import (
	"encoding/json"
	"strings"
	"testing"
)

// A setting left out has its default; a written zero or negative duration is
// refused, never taken as the default.
func TestAWrittenZeroOrNegativeHandshakeTimeoutIsRefused(t *testing.T) {
	for _, written := range []string{"0s", "-1s"} {
		var settings Settings
		if err := json.Unmarshal([]byte(strings.Replace(`{"handshake_timeout": "%s"}`, "%s", written, 1)), &settings); err != nil {
			t.Fatal(err)
		}
		if err := settings.Validate(); err == nil || !strings.Contains(err.Error(), "greater than zero") {
			t.Errorf("handshake_timeout %s: error = %v, want one saying it must be greater than zero", written, err)
		}
	}
}
