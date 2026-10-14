package types

import (
	"encoding/json"
	"fmt"

	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

// HTTPStatusRanges is a list of HTTP status codes written as one string:
// "204", "200-299" or "200,204,300-399". A bare number is refused, as with
// Duration: one spelling keeps a config file easy to read. The empty value,
// what a missing field leaves, means "not set", and each user says what that
// means for it.
type HTTPStatusRanges []httpstatusranges.Range

func (r HTTPStatusRanges) Ranges() []httpstatusranges.Range { return r }

func (r HTTPStatusRanges) MarshalJSON() ([]byte, error) {
	return json.Marshal(httpstatusranges.String(r))
}

// A JSON null leaves the receiver untouched, as stdlib unmarshalers do.
func (r *HTTPStatusRanges) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("status codes must be a quoted string such as \"204\" or \"200-299\", not %s: %w", data, err)
	}
	parsed, err := httpstatusranges.Parse(text)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}
