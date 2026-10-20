package blueprint

import (
	"time"

	"github.com/we-apranik/apranik/shared/delayedrequest"
	"github.com/we-apranik/apranik/spec"
)

type ProxyRequestTiming struct {
	MaxDelay *time.Duration

	Skip *ProxyRequestTimingSkip
}

type ProxyRequestTimingSkip struct {
	Enabled *bool

	Ports []int
}

func (t *ProxyRequestTiming) Options() spec.ProxyRequestTimingOptions {
	options := spec.ProxyRequestTimingOptions{MaxDelay: delayedrequest.DefaultMaxDelay}
	if t == nil {
		return options
	}
	if t.MaxDelay != nil {
		options.MaxDelay = *t.MaxDelay
	}
	if t.Skip == nil || t.Skip.Enabled == nil || !*t.Skip.Enabled {
		return options
	}
	if t.Skip.Ports == nil {
		options.SkipPorts = delayedrequest.DefaultSkipPorts()
		return options
	}
	for _, port := range t.Skip.Ports {
		options.SkipPorts = append(options.SkipPorts, uint16(port))
	}
	return options
}
