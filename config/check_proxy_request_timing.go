package config

import (
	"fmt"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/spec"
)

// An outbound's proxy_request_timing key.
func checkProxyRequestTiming(diagnostics *Diagnostics, outbound blueprint.Outbound, loc string) {
	timing := outbound.ProxyRequestTiming
	if timing == nil {
		return
	}
	timingLoc := fieldLocation(loc, "proxy_request_timing")

	if outbound.Proxy == nil {
		diagnostics.errorf(timingLoc, "proxy_request_timing is for a proxy outbound; freedom and groups send no request to a proxy server")
		return
	}
	if _, ok := outbound.Proxy.(spec.ProxyRequestTimingConfigurable); !ok {
		diagnostics.errorf(timingLoc, "proxy %q cannot carry proxy_request_timing", outbound.Proxy.Info().Name)
		return
	}

	if timing.MaxDelay != nil && *timing.MaxDelay <= 0 {
		diagnostics.errorf(fieldLocation(timingLoc, "max_delay"), "must be positive; leave it out for the default")
	}
	if timing.Skip != nil {
		checkProxyRequestTimingSkip(diagnostics, timing.Skip, fieldLocation(timingLoc, "skip"))
	}
}

func checkProxyRequestTimingSkip(diagnostics *Diagnostics, skip *blueprint.ProxyRequestTimingSkip, skipLoc string) {
	if skip.Ports == nil {
		return
	}
	portsLoc := fieldLocation(skipLoc, "ports")
	if len(skip.Ports) == 0 {
		diagnostics.errorf(portsLoc, "must name at least one port; leave it out for the built-in list")
		return
	}
	firstSeenAt := make(map[int]int, len(skip.Ports))
	for index, port := range skip.Ports {
		portLoc := fmt.Sprintf("%s[%d]", portsLoc, index)
		if port < 1 || port > 65535 {
			diagnostics.errorf(portLoc, "%d is not a port: use 1 to 65535", port)
			continue
		}
		if first, seen := firstSeenAt[port]; seen {
			diagnostics.errorf(portLoc, "%d is already listed at ports[%d]", port, first)
			continue
		}
		firstSeenAt[port] = index
	}
	if skip.Enabled == nil || !*skip.Enabled {
		diagnostics.warnf(portsLoc, "ports has no effect while skip.enabled is not true")
	}
}
