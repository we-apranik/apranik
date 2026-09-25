package types

import (
	"fmt"
	"io"
)

const redacted = "[redacted]"

// Secret is a string that refuses to print itself, so no error, log line or
// marshalled config carries a credential onto a seized device. Every fmt verb
// and json.Marshal render "[redacted]"; Reveal is the one way out.
//
// Because MarshalJSON redacts, a decoded config does not round-trip back to a
// usable config file. Configs are read, not rewritten.
type Secret string

func (s Secret) String() string { return redacted }

// Every verb, not only %s and %v: fmt consults Formatter before Stringer, so a
// type implementing only String still leaks under %q and %#v.
func (s Secret) Format(state fmt.State, verb rune) {
	if verb == 'q' {
		fmt.Fprintf(state, "%q", redacted)
		return
	}
	io.WriteString(state, redacted)
}

func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Named so a review can grep for every place a credential escapes.
func (s Secret) Reveal() string { return string(s) }
