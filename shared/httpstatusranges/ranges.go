// Package httpstatusranges reads a list of HTTP status codes written as text,
// such as "204" or "200,204,300-399", into inclusive ranges, so a probe, an API
// request and a config file spell the same thing the same way. It holds no
// default: whoever has none asks the user.
package httpstatusranges

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	lowestStatusCode  = 100
	highestStatusCode = 599
)

type Range struct {
	First int
	Last  int
}

// Parse reads codes and ranges separated by commas; spaces around a part are
// ignored. Duplicates and overlaps are fine. Empty text is an error, because
func Parse(text string) ([]Range, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("no status given; write codes and ranges such as 204 or 200-299")
	}
	parts := strings.Split(text, ",")
	ranges := make([]Range, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("an empty part between commas; write codes and ranges such as 200,204 or 200-299")
		}
		first, last, isRange := strings.Cut(part, "-")
		firstCode, err := parseStatusCode(first, part)
		if err != nil {
			return nil, err
		}
		lastCode := firstCode
		if isRange {
			if lastCode, err = parseStatusCode(last, part); err != nil {
				return nil, err
			}
		}
		if lastCode < firstCode {
			return nil, fmt.Errorf("status %q: the range ends before it starts", part)
		}
		ranges = append(ranges, Range{First: firstCode, Last: lastCode})
	}
	return ranges, nil
}

func parseStatusCode(text, part string) (int, error) {
	code, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("status %q: %q is not a number", part, strings.TrimSpace(text))
	}
	if code < lowestStatusCode || code > highestStatusCode {
		return 0, fmt.Errorf("status %q: a status code is %d to %d, not %d", part, lowestStatusCode, highestStatusCode, code)
	}
	return code, nil
}

func Contains(ranges []Range, code int) bool {
	for _, accepted := range ranges {
		if code >= accepted.First && code <= accepted.Last {
			return true
		}
	}
	return false
}

func String(ranges []Range) string {
	parts := make([]string, len(ranges))
	for index, accepted := range ranges {
		if accepted.First == accepted.Last {
			parts[index] = strconv.Itoa(accepted.First)
		} else {
			parts[index] = strconv.Itoa(accepted.First) + "-" + strconv.Itoa(accepted.Last)
		}
	}
	return strings.Join(parts, ",")
}
