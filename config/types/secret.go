package types

import (
	"fmt"
	"io"
)

const redacted = "[redacted]"

type Secret string

func (s Secret) String() string { return redacted }

// Every verb, not only %s and %v
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
