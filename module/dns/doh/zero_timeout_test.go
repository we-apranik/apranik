package doh

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAWrittenZeroOrNegativeTimeoutIsRefused(t *testing.T) {
	for _, written := range []string{"0s", "-1s"} {
		var settings Settings
		if err := json.Unmarshal([]byte(strings.Replace(`{"server": "1.1.1.1", "timeout": "%s"}`, "%s", written, 1)), &settings); err != nil {
			t.Fatal(err)
		}
		if err := settings.Validate(); err == nil || !strings.Contains(err.Error(), "greater than zero") {
			t.Errorf("timeout %s: error = %v, want one saying it must be greater than zero", written, err)
		}
	}
}
