package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHTTPStatusRangesDecodeFromOneString(t *testing.T) {
	var settings struct {
		Expected HTTPStatusRanges `json:"expected_status"`
	}
	if err := json.Unmarshal([]byte(`{"expected_status": "200,204,300-399"}`), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Expected) != 3 || settings.Expected.Ranges()[2].Last != 399 {
		t.Errorf("decoded %v", settings.Expected)
	}

	encoded, _ := json.Marshal(settings)
	if string(encoded) != `{"expected_status":"200,204,300-399"}` {
		t.Errorf("encoded %s", encoded)
	}
}

func TestHTTPStatusRangesRefuseABareNumberAndBadText(t *testing.T) {
	for input, want := range map[string]string{
		`{"e": 204}`:       `quoted string such as "204"`,
		`{"e": ["204"]}`:   "quoted string",
		`{"e": "700"}`:     "100 to 599",
		`{"e": "300-200"}`: "ends before it starts",
		`{"e": ""}`:        "no status given",
	} {
		var settings struct {
			E HTTPStatusRanges `json:"e"`
		}
		err := json.Unmarshal([]byte(input), &settings)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want one mentioning %q", input, err, want)
		}
	}
}

func TestHTTPStatusRangesLeaveTheValueAloneOnNullOrAbsence(t *testing.T) {
	var settings struct {
		E HTTPStatusRanges `json:"e"`
	}
	for _, input := range []string{`{}`, `{"e": null}`} {
		if err := json.Unmarshal([]byte(input), &settings); err != nil || len(settings.E) != 0 {
			t.Errorf("%s: %v, %v", input, settings.E, err)
		}
	}
}
