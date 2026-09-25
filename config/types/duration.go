// Package types holds the scalar values config files share, such as a Duration
// written as "30s" and a Secret that never prints itself. Modules may import
// it, so every module spells these values the same way.
package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration reads and writes as a JSON string -- "30s", "1h30m", "500ms" --
// rather than the bare nanosecond count time.Duration marshals to, so the unit
// is in the config file rather than in the reader's head.
//
// A bare number is refused: there is no defensible unit to assume, and
// guessing nanoseconds turns "keep_alive": 30 into something nobody intended.
type Duration time.Duration

func (d Duration) String() string { return time.Duration(d).String() }

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// A JSON null leaves the receiver untouched, as stdlib unmarshalers do, so it
// keeps whatever default the module's constructor populated.
func (d *Duration) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("d must be a quoted string such as \"30s\", not %s: %w", data, err)
	}

	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", text, err)
	}

	*d = Duration(parsed)
	return nil
}
