package blueprint

import (
	"time"

	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

type GroupType string

const (
	GroupTypeURLTest GroupType = "urltest"

	GroupTypeFallback GroupType = "fallback"

	GroupTypeSelector GroupType = "selector"
)

type Group struct {
	Type GroupType

	Members []string

	Probe Probe

	Tolerance *time.Duration
}

type Probe struct {
	URL string

	Interval *time.Duration
	Timeout  *time.Duration

	ExpectedStatuses []httpstatusranges.Range
}
