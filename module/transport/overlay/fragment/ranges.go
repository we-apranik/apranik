package fragment

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Range struct {
	Min, Max int
}

func (r *Range) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		return r.set(number, number)
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("a range is written as \"5\" or \"3-5\", not %s", data)
	}
	low, high, isRange := strings.Cut(strings.TrimSpace(text), "-")
	minimum, err := strconv.Atoi(strings.TrimSpace(low))
	if err != nil {
		return fmt.Errorf("a range is written as \"5\" or \"3-5\", not %q", text)
	}
	maximum := minimum
	if isRange {
		if maximum, err = strconv.Atoi(strings.TrimSpace(high)); err != nil {
			return fmt.Errorf("a range is written as \"5\" or \"3-5\", not %q", text)
		}
	}
	return r.set(minimum, maximum)
}

func (r *Range) set(minimum, maximum int) error {
	if minimum < 0 || maximum < minimum {
		return fmt.Errorf("range %d-%d: want 0 <= min <= max", minimum, maximum)
	}
	r.Min, r.Max = minimum, maximum
	return nil
}

func (r Range) MarshalJSON() ([]byte, error) {
	if r.Min == r.Max {
		return json.Marshal(strconv.Itoa(r.Min))
	}
	return json.Marshal(fmt.Sprintf("%d-%d", r.Min, r.Max))
}

func (r Range) pick() int {
	return r.Min + rand.IntN(r.Max-r.Min+1)
}

type DurationRange struct {
	Min, Max time.Duration
}

func (r *DurationRange) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("a delay is written as \"10ms\" or \"10-20ms\", not %s", data)
	}
	low, high, isRange := strings.Cut(strings.TrimSpace(text), "-")
	if !isRange {
		high = low
	}
	maximum, err := parseDelay(strings.TrimSpace(high))
	if err != nil {
		return err
	}
	low = strings.TrimSpace(low)
	if isRange && endsWithDigit(low) && !endsWithDigit(high) {
		low += strings.TrimLeftFunc(strings.TrimSpace(high), func(character rune) bool {
			return unicode.IsDigit(character) || character == '.'
		})
	}
	minimum, err := parseDelay(low)
	if err != nil {
		return err
	}
	if minimum < 0 || maximum < minimum {
		return fmt.Errorf("delay range %q: want 0 <= min <= max", text)
	}
	r.Min, r.Max = minimum, maximum
	return nil
}

func parseDelay(text string) (time.Duration, error) {
	if text == "0" {
		return 0, nil
	}
	delay, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("delay %q needs a unit, like \"10ms\" or \"10-20ms\"", text)
	}
	return delay, nil
}

func endsWithDigit(text string) bool {
	return text != "" && unicode.IsDigit(rune(text[len(text)-1]))
}

func (r DurationRange) MarshalJSON() ([]byte, error) {
	if r.Min == r.Max {
		return json.Marshal(r.Min.String())
	}
	return json.Marshal(r.Min.String() + "-" + r.Max.String())
}

func (r DurationRange) pick() time.Duration {
	if r.Max == r.Min {
		return r.Min
	}
	return r.Min + rand.N(r.Max-r.Min+1)
}

func entryFor[Entry any](entries []Entry, pieceIndex int) Entry {
	return entries[min(pieceIndex, len(entries)-1)]
}
