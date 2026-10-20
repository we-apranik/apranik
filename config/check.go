package config

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/udp"
	"github.com/we-apranik/apranik/core/net/udp/nat"
	"github.com/we-apranik/apranik/core/net/udp/session"
	"github.com/we-apranik/apranik/spec"
)

func Check(coreBlueprint *blueprint.Blueprint) Diagnostics {
	var diagnostics Diagnostics
	if coreBlueprint == nil {
		diagnostics.errorf("", "no coreBlueprint to check")
		return diagnostics
	}

	checkLog(&diagnostics, coreBlueprint.Log, "log")
	checkTags(&diagnostics, coreBlueprint)

	for index, inbound := range coreBlueprint.Inbounds {
		checkInbound(&diagnostics, inbound, indexLocation("inbounds", index))
	}
	for index, outbound := range coreBlueprint.Outbounds {
		checkOutbound(&diagnostics, outbound, indexLocation("outbounds", index))
	}
	datasetKinds := checkDatasets(&diagnostics, coreBlueprint.Datasets)
	checkDNS(&diagnostics, coreBlueprint, datasetKinds)
	checkGroups(&diagnostics, coreBlueprint)
	checkRouting(&diagnostics, coreBlueprint, datasetKinds)
	checkStats(&diagnostics, coreBlueprint)
	checkAPIs(&diagnostics, coreBlueprint)

	if len(coreBlueprint.Inbounds) == 0 {
		diagnostics.warnf("inbounds", "no inbounds configured, so nothing will accept traffic")
	}
	if len(coreBlueprint.Outbounds) == 0 {
		diagnostics.warnf("outbounds", "no outbounds configured, so nothing can be reached")
	}

	return diagnostics
}

// Silence is the default, not an oversight
func checkLog(diagnostics *Diagnostics, log logging.Config, loc string) {
	if len(log.Outputs) == 0 {
		diagnostics.warnf(loc, "no log outputs configured, so nothing will be logged")
	}
	for index, output := range log.Outputs {
		if err := output.Validate(); err != nil {
			diagnostics.report(indexLocation(fieldLocation(loc, "outputs"), index), err)
		}
	}
}

func checkTags(diagnostics *Diagnostics, coreBlueprint *blueprint.Blueprint) {
	seen := make(map[string]int, len(coreBlueprint.Inbounds))
	for index, inbound := range coreBlueprint.Inbounds {
		loc := fieldLocation(indexLocation("inbounds", index), "tag")
		if inbound.Tag == "" {
			diagnostics.errorf(loc, "an inbound needs a tag")
			continue
		}
		if first, duplicate := seen[inbound.Tag]; duplicate {
			diagnostics.errorf(loc, "tag %q is already used by inbounds[%d]", inbound.Tag, first)
			continue
		}
		seen[inbound.Tag] = index
	}

	seen = make(map[string]int, len(coreBlueprint.Outbounds))
	for index, outbound := range coreBlueprint.Outbounds {
		loc := fieldLocation(indexLocation("outbounds", index), "tag")
		if outbound.Tag == "" {
			diagnostics.errorf(loc, "an outbound needs a tag")
			continue
		}
		if outbound.Tag == blueprint.DefaultDNSOutboundTag {
			diagnostics.errorf(loc, "tag %q is reserved for the freedom core builds for dns servers that name no outbound", outbound.Tag)
			continue
		}
		if first, duplicate := seen[outbound.Tag]; duplicate {
			diagnostics.errorf(loc, "tag %q is already used by outbounds[%d]", outbound.Tag, first)
			continue
		}
		seen[outbound.Tag] = index
	}
}

func checkInbound(diagnostics *Diagnostics, inbound blueprint.Inbound, loc string) {
	if !inbound.Bind.IsValid() {
		diagnostics.errorf(fieldLocation(loc, "listen"), "an inbound must say where to listen; there is no default")
	} else if inbound.Bind.Port() == 0 {
		diagnostics.errorf(fieldLocation(loc, "port"), "an inbound must say which port to listen on")
	}

	switch inbound.Network {
	case spec.NetworkUDP:
		if inbound.DatagramTransport.IsZero() {
			diagnostics.errorf(fieldLocation(loc, "transport"), "a udp inbound needs a base datagram transport first in its stack")
		}
		if inbound.DatagramProxy == nil {
			diagnostics.errorf(fieldLocation(loc, "protocol"), "a udp inbound needs a datagram proxy protocol")
		}
		if !inbound.StreamTransport.IsZero() || inbound.StreamProxy != nil || inbound.Mixed != nil {
			diagnostics.errorf(fieldLocation(loc, "network"), "network is udp but a stream transport or proxy was configured")
		}

	default:
		if inbound.StreamTransport.IsZero() {
			diagnostics.errorf(fieldLocation(loc, "transport"), "an inbound needs a base stream transport first in its stack")
		}
		if inbound.StreamProxy == nil && inbound.Mixed == nil {
			diagnostics.errorf(fieldLocation(loc, "protocol"), "an inbound needs a proxy protocol")
		}
		if inbound.StreamProxy != nil && inbound.Mixed != nil {
			diagnostics.errorf(fieldLocation(loc, "protocol"), "an inbound has one protocol: a proxy protocol or mixed, not both")
		}
		if !inbound.DatagramTransport.IsZero() || inbound.DatagramProxy != nil {
			diagnostics.errorf(fieldLocation(loc, "network"), "network is tcp but a datagram transport or proxy was configured")
		}
	}

	transportLoc := fieldLocation(loc, "transport")
	checkStreamTransportStack(diagnostics, inbound.StreamTransport, transportLoc)
	for index, overlay := range inbound.StreamTransport.Overlays {
		if needsNoServerSide(overlay) {
			diagnostics.errorf(indexLocation(transportLoc, index+1),
				"%q has no server side; it only changes outgoing connections, so it does nothing on an inbound. Remove it here", overlay.Info().Name)
		}
	}
	checkDatagramTransportStack(diagnostics, inbound.DatagramTransport, transportLoc)

	collectWarnings(diagnostics, inbound.StreamProxy, fieldLocation(loc, "protocol", "settings"))
	collectWarnings(diagnostics, inbound.DatagramProxy, fieldLocation(loc, "protocol", "settings"))
	if inbound.Mixed != nil {
		collectWarnings(diagnostics, inbound.Mixed.SOCKS5, fieldLocation(loc, "protocol", "settings", mixedSOCKS5Module))
		collectWarnings(diagnostics, inbound.Mixed.HTTP, fieldLocation(loc, "protocol", "settings", mixedHTTPModule))
		if len(inbound.Mixed.Users) > 0 {
			diagnostics.warnf(fieldLocation(loc, "protocol", "settings", "users"),
				"users log in with a password that SOCKS5 and HTTP Basic send in a recoverable form unless the carrier below is encrypted")
		}
	}

	if inbound.Bind.IsValid() {
		for index, overlay := range inbound.StreamTransport.Overlays {
			collectListenAddressWarnings(diagnostics, overlay, inbound.Bind, fieldLocation(indexLocation(transportLoc, index+1), "settings"))
		}
		collectListenAddressWarnings(diagnostics, inbound.StreamProxy, inbound.Bind, fieldLocation(loc, "protocol", "settings"))
		collectListenAddressWarnings(diagnostics, inbound.DatagramProxy, inbound.Bind, fieldLocation(loc, "protocol", "settings"))
		// With users the halves have none of their own, and warning that they have no users would be wrong.
		if inbound.Mixed != nil && len(inbound.Mixed.Users) == 0 {
			collectListenAddressWarnings(diagnostics, inbound.Mixed.SOCKS5, inbound.Bind, fieldLocation(loc, "protocol", "settings", mixedSOCKS5Module))
			collectListenAddressWarnings(diagnostics, inbound.Mixed.HTTP, inbound.Bind, fieldLocation(loc, "protocol", "settings", mixedHTTPModule))
		}
	}

	checkInboundMuxCool(diagnostics, inbound, loc)

	checkLimitIsPositive(diagnostics, fieldLocation(loc, "max_tcp_connections"), inbound.MaxTCPConnections)
	if inbound.MaxTCPConnections != nil && inbound.Network == spec.NetworkUDP {
		diagnostics.errorf(fieldLocation(loc, "max_tcp_connections"), "limits TCP connections; a udp inbound has none")
	}
	checkLimitIsPositive(diagnostics, fieldLocation(loc, "max_udp_sessions"), inbound.MaxUDPSessions)

	if inbound.TCPIdleTimeout != nil && *inbound.TCPIdleTimeout == 0 {
		diagnostics.errorf(fieldLocation(loc, "tcp_idle_timeout"),
			"must not be zero: leave it out for the default, or write a negative duration to disable the bound")
	}
	if inbound.UDPSessionIdleTimeout != nil && *inbound.UDPSessionIdleTimeout <= 0 {
		diagnostics.errorf(fieldLocation(loc, "udp_session_idle_timeout"),
			"must be greater than zero (leave it out for the default, %s): a UDP session has no other end, so it would never be released", session.DefaultIdleTimeout)
	}

	sessionIdle := effectiveUDPSessionIdleTimeout(inbound)
	checkUDPEntryIdleTimeout(diagnostics, fieldLocation(loc, "udp_routing_cache_entry_idle_timeout"),
		inbound.UDPRoutingCacheEntryIdleTimeout, udp.DefaultRoutingCacheEntryIdleTimeout, sessionIdle,
		"a destination silent for this long is forgotten, and a UDP session has no other way to forget one")
	checkUDPMaxDatagramSize(diagnostics, fieldLocation(loc, "udp_max_datagram_size"), inbound.UDPMaxDatagramSize)
	checkUDPMaxEntriesPerSession(diagnostics, fieldLocation(loc, "udp_routing_cache_max_entries_per_session"),
		inbound.UDPRoutingCacheMaxEntriesPerSession, udp.DefaultRoutingCacheMaxEntriesPerSession)

	if inbound.Fallback.IsValid() {
		if inbound.Network == spec.NetworkUDP {
			diagnostics.errorf(fieldLocation(loc, "fallback"), "a fallback takes a refused stream; a udp inbound has none")
		}
		if inbound.Fallback.Port() == 0 {
			diagnostics.errorf(fieldLocation(loc, "fallback", "server_port"), "a fallback needs a port")
		}
	}
}

func checkOutbound(diagnostics *Diagnostics, outbound blueprint.Outbound, loc string) {
	protocolLoc := fieldLocation(loc, "protocol")

	protocols := 0
	for _, set := range []bool{outbound.Proxy != nil, outbound.Freedom != nil, outbound.Group != nil} {
		if set {
			protocols++
		}
	}
	switch {
	case protocols == 0:
		diagnostics.errorf(protocolLoc, "an outbound needs a protocol: a proxy protocol, freedom or a group (%s, %s, %s)", urlTestProtocol, fallbackProtocol, selectorProtocol)
		return
	case protocols > 1:
		diagnostics.errorf(protocolLoc, "an outbound has exactly one protocol")
		return
	}

	checkMuxCool(diagnostics, outbound, loc)
	checkProxyRequestTiming(diagnostics, outbound, loc)

	if outbound.Group != nil {
		checkGroupOutboundFields(diagnostics, outbound, loc)
		return
	}
	if outbound.Proxy != nil {
		checkProxyOutbound(diagnostics, outbound, loc)
		return
	}

	if outbound.Server.IsValid() {
		diagnostics.errorf(fieldLocation(loc, "server"), "%s reaches no proxy server, so it takes no server", freedomProtocol)
	}
	if outbound.DNSServer != "" {
		diagnostics.errorf(fieldLocation(loc, "dns_server"),
			"%s has no server name of its own to resolve; each destination is resolved by the dns rules", freedomProtocol)
	}
	if !outbound.StreamTransport.IsZero() {
		diagnostics.errorf(fieldLocation(loc, "transport"), "freedom's transport belongs in its own stack, Freedom.StreamTransport")
	}

	settingsLoc := fieldLocation(protocolLoc, "settings")
	transportLoc := fieldLocation(loc, "transport")
	checkStreamTransportStack(diagnostics, outbound.Freedom.StreamTransport, transportLoc)
	for index, overlay := range outbound.Freedom.StreamTransport.Overlays {
		if !needsNoServerSide(overlay) {
			name := overlay.Info().Name
			diagnostics.errorf(indexLocation(transportLoc, index+1),
				"%q cannot be used with freedom. Freedom connects straight to each destination, and a destination does not run %q; only transports that need no server side work there", name, name)
		}
	}
	collectWarnings(diagnostics, outbound.Freedom.DatagramTransport.Base, fieldLocation(settingsLoc, "udp"))
	checkUDPMaxDatagramSize(diagnostics, fieldLocation(settingsLoc, "nat", "max_datagram_size"), outbound.Freedom.NATMaxDatagramSize)
	if outbound.Freedom.NAT != nil {
		checkUDPPolicy(diagnostics, *outbound.Freedom.NAT, fieldLocation(settingsLoc, "nat"))
	}
}

func checkProxyOutbound(diagnostics *Diagnostics, outbound blueprint.Outbound, loc string) {
	name := outbound.Proxy.Info().Name
	if !outbound.Server.IsValid() {
		diagnostics.errorf(fieldLocation(loc, "server"), "proxy %q has no server address to reach", name)
	} else if outbound.Server.Port() == 0 {
		diagnostics.errorf(fieldLocation(loc, "server_port"), "proxy %q has no server port", name)
	}
	if outbound.StreamTransport.IsZero() {
		diagnostics.errorf(fieldLocation(loc, "transport"), "proxy %q needs a transport stack to reach its server", name)
	}

	checkStreamTransportStack(diagnostics, outbound.StreamTransport, fieldLocation(loc, "transport"))
	collectWarnings(diagnostics, outbound.Proxy, fieldLocation(loc, "protocol", "settings"))
}

func checkUDPPolicy(diagnostics *Diagnostics, policy nat.Policy, loc string) {
	if policy.Filtering() == nat.EndpointIndependentFiltering {
		diagnostics.warnf(fieldLocation(loc, "filtering"),
			"endpoint-independent filtering accepts packets from any sender once a mapping exists, which lets an off-path attacker inject traffic into the session")
	}
}

func checkStreamTransportStack(diagnostics *Diagnostics, stack blueprint.StreamTransportStack, loc string) {
	if stack.Base == nil {
		return
	}
	collectWarnings(diagnostics, stack.Base, fieldLocation(indexLocation(loc, 0), "settings"))

	seen := make(map[string]int, len(stack.Overlays))
	for index, overlay := range stack.Overlays {
		layerLoc := indexLocation(loc, index+1)
		collectWarnings(diagnostics, overlay, fieldLocation(layerLoc, "settings"))

		name := overlay.Info().Name
		if first, repeated := seen[name]; repeated {
			diagnostics.warnf(layerLoc, "overlay %q already appears at index %d of this stack", name, first+1)
			continue
		}
		seen[name] = index
	}
	checkApplicationProtocols(diagnostics, stack.Overlays, loc)
}

func checkApplicationProtocols(diagnostics *Diagnostics, overlays []spec.OverlayStreamTransportModule, loc string) {
	for index := 0; index+1 < len(overlays); index++ {
		lower, upper := overlays[index], overlays[index+1]
		offered := transportApplicationProtocols(lower)
		speakable := transportApplicationProtocols(upper)
		if len(offered) == 0 || len(speakable) == 0 {
			continue
		}
		for _, protocol := range offered {
			if !slices.Contains(speakable, protocol) {
				diagnostics.errorf(fieldLocation(indexLocation(loc, index+1), "settings", "alpn"),
					"%s offers %v, but %s above it can speak only %v, and the other end may choose %s; set alpn to %v",
					lower.Info().Name, offered, upper.Info().Name, speakable, protocol, quotedList(speakable))
				break
			}
		}
	}
}

func transportApplicationProtocols(overlay spec.OverlayStreamTransportModule) []string {
	if provider, provides := overlay.(spec.TransportInfoProvider); provides {
		return provider.TransportInfo().ApplicationProtocols
	}
	return nil
}

// Declared by the transport itself, so no transport is named here.
func needsNoServerSide(overlay spec.OverlayStreamTransportModule) bool {
	provider, provides := overlay.(spec.TransportInfoProvider)
	return provides && provider.TransportInfo().NeedsNoServerSide
}

func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = strconv.Quote(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func checkDatagramTransportStack(diagnostics *Diagnostics, stack blueprint.DatagramTransportStack, loc string) {
	if stack.Base == nil {
		return
	}
	collectWarnings(diagnostics, stack.Base, fieldLocation(indexLocation(loc, 0), "settings"))

	seen := make(map[string]int, len(stack.Overlays))
	for index, overlay := range stack.Overlays {
		layerLoc := indexLocation(loc, index+1)
		collectWarnings(diagnostics, overlay, fieldLocation(layerLoc, "settings"))

		name := overlay.Info().Name
		if first, repeated := seen[name]; repeated {
			diagnostics.warnf(layerLoc, "overlay %q already appears at index %d of this stack", name, first+1)
			continue
		}
		seen[name] = index
	}
}

// The module knows what is worth warning about; only the caller knows where in the config file it came from.
func collectWarnings(diagnostics *Diagnostics, component spec.Config, loc string) {
	if component == nil {
		return
	}
	for _, warning := range component.Warnings() {
		diagnostics.warnf(loc, "%s", warning)
	}
}

// Only what depends on where the inbound listens
func collectListenAddressWarnings(diagnostics *Diagnostics, component any, listen spec.Endpoint, loc string) {
	warner, implements := component.(spec.ListenAddressWarner)
	if !implements || warner == nil {
		return
	}
	for _, warning := range warner.WarningsForListenAddress(listen) {
		diagnostics.warnf(loc, "%s", warning)
	}
}

func checkUDPEntryIdleTimeout(diagnostics *Diagnostics, location string, configured *time.Duration, defaultValue, sessionIdle time.Duration, whyItMustBePositive string) {
	if configured == nil {
		return
	}
	if *configured <= 0 {
		diagnostics.errorf(location, "must be greater than zero (leave it out for the default, %s): %s", defaultValue, whyItMustBePositive)
		return
	}
	if *configured > sessionIdle {
		diagnostics.warnf(location, "%s is longer than udp_session_idle_timeout (%s): the session ends first, so this setting never takes effect",
			*configured, sessionIdle)
	}
}

func checkUDPMaxEntriesPerSession(diagnostics *Diagnostics, location string, configured *int, defaultValue int) {
	if configured != nil && *configured <= 0 {
		diagnostics.errorf(location, "must be greater than zero (leave it out for the default, %d)", defaultValue)
	}
}

// One beyond the largest UDP payload could never fill, and would only waste memory.
func checkUDPMaxDatagramSize(diagnostics *Diagnostics, location string, configured *int) {
	if configured == nil {
		return
	}
	switch {
	case *configured <= 0:
		diagnostics.errorf(location, "must be greater than zero (leave it out for the default, %d)", udp.DefaultMaxDatagramSize)
	case *configured > udp.LargestDatagramSize:
		diagnostics.errorf(location, "%d is more than a UDP datagram can hold (%d)", *configured, udp.LargestDatagramSize)
	case *configured < 512:
		diagnostics.warnf(location, "%d bytes is below the smallest datagram a DNS message needs (512): such traffic is dropped", *configured)
	}
}

func effectiveUDPSessionIdleTimeout(inbound blueprint.Inbound) time.Duration {
	if inbound.UDPSessionIdleTimeout != nil && *inbound.UDPSessionIdleTimeout > 0 {
		return *inbound.UDPSessionIdleTimeout
	}
	return session.DefaultIdleTimeout
}

// A limit that was left out is no limit; zero is not a way to say so.
func checkLimitIsPositive(diagnostics *Diagnostics, location string, configured *int) {
	if configured != nil && *configured <= 0 {
		diagnostics.errorf(location, "must be greater than zero; leave it out for no limit")
	}
}
