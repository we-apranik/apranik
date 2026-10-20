package types

import (
	"encoding/json"
	"fmt"
	"time"
)

type Duration time.Duration

func (d Duration) String() string { return time.Duration(d).String() }

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

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
