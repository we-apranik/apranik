package blueprint

import (
	"time"

	"github.com/The-Apranik/Apranik/shared/delayedrequest"
	"github.com/The-Apranik/Apranik/spec"
)

// ProxyRequestTiming is an outbound's proxy_request_timing key. Every field is
// optional, and a field left out takes its default, so an outbound with none
// holds each request up to the default delay and skips no port. Core reads it; a
// protocol sees only the options it makes.
type ProxyRequestTiming struct {
	// Default delayedrequest.DefaultMaxDelay; Check refuses zero and negative.
	MaxDelay *time.Duration

	Skip *ProxyRequestTimingSkip
}

type ProxyRequestTimingSkip struct {
	// Default false: no port sends its request at once.
	Enabled *bool

	// Nil means delayedrequest.DefaultSkipPorts. Check refuses an empty list,
	// a number outside 1 to 65535 and a duplicate, which is why Options may
	// convert without checking.
	Ports []int
}

// Options resolves the defaults into what a protocol is told. A nil
// ProxyRequestTiming is the defaults.
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
