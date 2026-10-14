package blueprint

import (
	"time"

	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

// GroupType is what a group does with its members.
type GroupType string

const (
	// GroupTypeURLTest sends new connections to the member that answered its
	// probe fastest.
	GroupTypeURLTest GroupType = "urltest"

	// GroupTypeFallback sends new connections to the first member, in list
	// order, that passed its probe.
	GroupTypeFallback GroupType = "fallback"

	// GroupTypeSelector sends new connections to the member a person chose,
	// the first until then. It takes no probe.
	GroupTypeSelector GroupType = "selector"
)

// Group is an outbound that connects nowhere itself: it picks one of its
// members for each connection. A member is another outbound, possibly another
// group, named by its tag.
type Group struct {
	Type GroupType

	// Tags of the member outbounds, in the order that breaks ties.
	Members []string

	Probe Probe

	// Only for GroupTypeURLTest: how much faster than the current member
	// another must be before the group switches to it. Nil means the
	// group's own default; a value must be positive.
	Tolerance *time.Duration
}

// Probe is one request sent through each member to learn whether it works and
// how long it takes. A field left out (nil) takes the group's own default; a
// written zero or negative duration is refused.
type Probe struct {
	// Must be http or https; its scheme decides whether the request is encrypted.
	URL string

	Interval *time.Duration
	Timeout  *time.Duration

	// The statuses that count as a pass.
	ExpectedStatuses []httpstatusranges.Range
}
