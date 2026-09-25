package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDurationRoundTrip(t *testing.T) {
	for _, text := range []string{"30s", "1m30s", "1h30m0s", "500ms", "0s"} {
		parsed, err := time.ParseDuration(text)
		if err != nil {
			t.Fatalf("test case %q is not a duration: %v", text, err)
		}

		encoded, err := json.Marshal(Duration(parsed))
		if err != nil {
			t.Fatalf("marshal %q: %v", text, err)
		}
		if string(encoded) != `"`+text+`"` {
			t.Errorf("marshal %q = %s, want %q", text, encoded, `"`+text+`"`)
		}

		var decoded Duration
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", encoded, err)
		}
		if decoded != Duration(parsed) {
			t.Errorf("round trip %q = %v, want %v", text, decoded, parsed)
		}
	}
}

// The point of the type: the value a module reads is a real time.Duration.
func TestDurationConvertsToTimeDuration(t *testing.T) {
	var decoded Duration
	if err := json.Unmarshal([]byte(`"90s"`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if time.Duration(decoded) != 90*time.Second {
		t.Errorf("time.Duration(decoded) = %v, want 90s", time.Duration(decoded))
	}
}

func TestDurationRejectsBareNumber(t *testing.T) {
	var decoded Duration
	err := json.Unmarshal([]byte(`30`), &decoded)
	if err == nil {
		t.Fatal("unmarshal of a bare number succeeded, want an error")
	}
	if !strings.Contains(err.Error(), `"30s"`) {
		t.Errorf("error %q does not show the accepted form", err)
	}
	if decoded != 0 {
		t.Errorf("receiver was written despite the error: %v", decoded)
	}
}

func TestDurationRejectsUnparsableString(t *testing.T) {
	var decoded Duration
	err := json.Unmarshal([]byte(`"half an hour"`), &decoded)
	if err == nil {
		t.Fatal("unmarshal of an unparsable string succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "half an hour") {
		t.Errorf("error %q does not name the offending value", err)
	}
}

// A field absent from the config file keeps the default the constructor set;
// null does the same, and an explicit value overrides it.
func TestDurationDecodeIntoPopulatedStruct(t *testing.T) {
	type settings struct {
		KeepAlive Duration `json:"keep_alive"`
	}

	cases := []struct {
		document string
		want     time.Duration
	}{
		{`{}`, 15 * time.Second},
		{`{"keep_alive":null}`, 15 * time.Second},
		{`{"keep_alive":"30s"}`, 30 * time.Second},
		{`{"keep_alive":"0s"}`, 0},
	}

	for _, testCase := range cases {
		decoded := settings{KeepAlive: Duration(15 * time.Second)}
		if err := json.Unmarshal([]byte(testCase.document), &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", testCase.document, err)
		}
		if time.Duration(decoded.KeepAlive) != testCase.want {
			t.Errorf("%s gave %v, want %v", testCase.document, decoded.KeepAlive, testCase.want)
		}
	}
}
