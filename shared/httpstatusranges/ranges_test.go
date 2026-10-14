package httpstatusranges_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

func TestParseReadsCodesRangesAndLists(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []httpstatusranges.Range
	}{
		{"204", []httpstatusranges.Range{{204, 204}}},
		{"200-299", []httpstatusranges.Range{{200, 299}}},
		{"200,204,300-399", []httpstatusranges.Range{{200, 200}, {204, 204}, {300, 399}}},
		{" 200 , 204 - 206 ", []httpstatusranges.Range{{200, 200}, {204, 206}}},
		{"200,200-299", []httpstatusranges.Range{{200, 200}, {200, 299}}},
		{"100-599", []httpstatusranges.Range{{100, 599}}},
	} {
		got, err := httpstatusranges.Parse(tc.text)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("Parse(%q) = %v, %v; want %v", tc.text, got, err, tc.want)
		}
	}
}

func TestParseRefusesWhatIsNotAStatusList(t *testing.T) {
	for text, want := range map[string]string{
		"":          "no status given",
		"   ":       "no status given",
		"99":        "100 to 599",
		"600":       "100 to 599",
		"700":       "100 to 599",
		"abc":       "not a number",
		"2xx":       "not a number",
		"300-200":   "ends before it starts",
		"200,,204":  "empty part",
		"200,":      "empty part",
		"200-":      "not a number",
		"-299":      "not a number",
		"200-299-3": "not a number",
		"*":         "not a number",
	} {
		_, err := httpstatusranges.Parse(text)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want one mentioning %q", text, err, want)
		}
	}
}

func TestContains(t *testing.T) {
	ranges, _ := httpstatusranges.Parse("200-299,301")
	for code, want := range map[int]bool{199: false, 200: true, 204: true, 299: true, 300: false, 301: true, 302: false, 500: false} {
		if got := httpstatusranges.Contains(ranges, code); got != want {
			t.Errorf("Contains(%d) = %v, want %v", code, got, want)
		}
	}
	if httpstatusranges.Contains(nil, 200) {
		t.Error("an empty list contains a code")
	}
}

func TestStringWritesWhatParseReads(t *testing.T) {
	for _, text := range []string{"204", "200-299", "200,204,300-399"} {
		ranges, _ := httpstatusranges.Parse(text)
		if got := httpstatusranges.String(ranges); got != text {
			t.Errorf("String(Parse(%q)) = %q", text, got)
		}
	}
}
