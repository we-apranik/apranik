package trojan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A setting left out has its default; a written zero or negative duration is
// refused, never taken as the default.
func TestAWrittenZeroOrNegativeHandshakeTimeoutIsRefused(t *testing.T) {
	for _, written := range []string{"0s", "-1s"} {
		var settings Settings
		if err := json.Unmarshal([]byte(strings.Replace(`{"password": "p", "handshake_timeout": "%s"}`, "%s", written, 1)), &settings); err != nil {
			t.Fatal(err)
		}
		if err := settings.Validate(); err == nil || !strings.Contains(err.Error(), "greater than zero") {
			t.Errorf("handshake_timeout %s: error = %v, want one saying it must be greater than zero", written, err)
		}
	}
}

func TestAWrittenZeroOrNegativeMaxFirstPayloadDelayIsRefused(t *testing.T) {
	for _, written := range []string{"0s", "-1s"} {
		var settings Settings
		if err := json.Unmarshal([]byte(strings.Replace(`{"password": "p", "max_first_payload_delay": "%s"}`, "%s", written, 1)), &settings); err != nil {
			t.Fatal(err)
		}
		if err := settings.Validate(); err == nil || !strings.Contains(err.Error(), "max_first_payload_delay") || !strings.Contains(err.Error(), "greater than zero") {
			t.Errorf("max_first_payload_delay %s: error = %v, want one naming the key and saying it must be greater than zero", written, err)
		}
	}
}

func TestMaxFirstPayloadDelayDefaultsToFourHundredMilliseconds(t *testing.T) {
	var settings Settings
	if got := settings.maxFirstPayloadDelay(); got != 400*time.Millisecond {
		t.Errorf("default = %v, want 400ms", got)
	}
}
