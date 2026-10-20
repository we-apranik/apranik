package group

import (
	"time"

	"github.com/we-apranik/apranik/shared/httpstatusranges"
)

// What a group does when its settings leave a field out, all in one place. They
// belong to the groups, not to measuring: the DelayTester has no defaults.
// PROVISIONAL.
const (
	defaultGroupProbeURL      = "https://www.gstatic.com/generate_204"
	defaultGroupProbeInterval = 5 * time.Minute
	defaultGroupProbeTimeout  = 10 * time.Second

	defaultURLTestTolerance = 50 * time.Millisecond
)

func defaultGroupProbeExpectedStatuses() []httpstatusranges.Range {
	return []httpstatusranges.Range{{First: 200, Last: 299}}
}
