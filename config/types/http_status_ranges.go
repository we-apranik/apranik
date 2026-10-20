package types

import (
	"encoding/json"
	"fmt"

	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

type HTTPStatusRanges []httpstatusranges.Range

func (r HTTPStatusRanges) Ranges() []httpstatusranges.Range { return r }

func (r HTTPStatusRanges) MarshalJSON() ([]byte, error) {
	return json.Marshal(httpstatusranges.String(r))
}

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
