package blueprint

import "time"

// GroupType is what a group does with its members.
type GroupType string

const (
	// GroupTypeURLTest sends new connections to the member that answered its
	// probe fastest.
	GroupTypeURLTest GroupType = "urltest"

	// GroupTypeFallback sends new connections to the first member, in list
	// order, that passed its probe.
	GroupTypeFallback GroupType = "fallback"
)

// ProbeType is how a group tests a member.
type ProbeType string

const (
	ProbeTypeHTTP  ProbeType = "http"
	ProbeTypeHTTPS ProbeType = "https"
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
	// another must be before the group switches to it. Zero means
	// outbound.DefaultGroupTolerance.
	Tolerance time.Duration
}

// Probe is one request sent through each member to learn whether it works and
// how long it takes. Zero fields mean the defaults in package outbound; a
// negative duration is refused.
type Probe struct {
	Type ProbeType

	// Empty means outbound.DefaultHTTPProbeURL or DefaultHTTPSProbeURL.
	URL string

	Interval time.Duration
	Timeout  time.Duration
}
