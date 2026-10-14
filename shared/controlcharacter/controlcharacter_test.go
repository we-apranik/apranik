package controlcharacter_test

import (
	"testing"

	"github.com/we-apranik/apranik/shared/controlcharacter"
)

func TestContains(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		want bool
	}{
		"empty":              {"", false},
		"plain":              {"sara", false},
		"a space":            {"sara omid", false},
		"a colon":            {"sara:omid", false},
		"persian":            {"سارا", false},
		"emoji":              {"sara 🙂", false},
		"a newline":          {"sara\n", true},
		"a carriage return":  {"sara\rx", true},
		"a tab":              {"sara\tx", true},
		"a nul":              {"pa\x00ss", true},
		"the escape":         {"\x1b[31m", true},
		"delete":             {"sara\x7f", true},
		"the last allowed":   {"sara~", false},
		"the first allowed":  {" sara", false},
		"a latin-1 control":  {"sara\u0085", false},
		"a control at start": {"\x01sara", true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := controlcharacter.Contains(tc.text); got != tc.want {
				t.Errorf("Contains(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
