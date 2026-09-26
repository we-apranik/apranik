package config

import (
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/spec"
)

// Check reports what a Blueprint has but cannot run on: a missing field, a
// conflicting pair, an unsafe value. It takes no registry and constructs,
// opens and binds nothing, so it holds for a Blueprint however it was made
// and "apranik check" can verify a config file on a machine that could never
// run it.
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

	if len(coreBlueprint.Inbounds) == 0 {
		diagnostics.warnf("inbounds", "no inbounds configured, so nothing will accept traffic")
	}
	if len(coreBlueprint.Outbounds) == 0 {
		diagnostics.warnf("outbounds", "no outbounds configured, so nothing can be reached")
	}

	return diagnostics
}

// Silence is the default, not an oversight: a log naming what a user reached
// is evidence on a seized device. It is still worth one warning.
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

// Inbounds and outbounds are separate namespaces.
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

	// The typed stacks already make a stream overlay over a datagram base
	// unrepresentable, so what is left is that the right one was filled.
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
	checkDatagramTransportStack(diagnostics, inbound.DatagramTransport, transportLoc)

	collectWarnings(diagnostics, inbound.StreamProxy, fieldLocation(loc, "protocol", "settings"))
	collectWarnings(diagnostics, inbound.DatagramProxy, fieldLocation(loc, "protocol", "settings"))
	if inbound.Mixed != nil {
		collectWarnings(diagnostics, inbound.Mixed.SOCKS5, fieldLocation(loc, "protocol", "settings", mixedSOCKS5Module))
		collectWarnings(diagnostics, inbound.Mixed.HTTP, fieldLocation(loc, "protocol", "settings", mixedHTTPModule))
	}

	if inbound.MaxConnections < 0 {
		diagnostics.errorf(fieldLocation(loc, "max_connections"), "must not be negative; 0 means no limit")
	}
	if inbound.MaxConnections > 0 && inbound.Network == spec.NetworkUDP {
		diagnostics.errorf(fieldLocation(loc, "max_connections"), "limits TCP connections; a udp inbound has none")
	}

	if inbound.UDPSessionIdleTimeout < 0 {
		diagnostics.errorf(fieldLocation(loc, "udp_session_idle_timeout"),
			"must not be negative: a UDP session has no other end, so it would never be released")
	}

	if inbound.Fallback.IsValid() {
		if inbound.Network == spec.NetworkUDP {
			diagnostics.errorf(fieldLocation(loc, "fallback"), "a fallback takes a refused stream; a udp inbound has none")
		}
		if inbound.Fallback.Port() == 0 {
			diagnostics.errorf(fieldLocation(loc, "fallback", "server_port"), "a fallback needs a port")
		}
	}

	if inbound.Sniffing != nil {
		checkSniffing(diagnostics, *inbound.Sniffing, inbound.Network, fieldLocation(loc, "sniffing"))
	}
}

func checkSniffing(diagnostics *Diagnostics, sniffing blueprint.Sniffing, network spec.Network, loc string) {
	if network == spec.NetworkUDP {
		diagnostics.errorf(loc, "sniffing works on tcp inbounds only for now")
	}
	if len(sniffing.Sniffers) == 0 {
		diagnostics.errorf(fieldLocation(loc, "sniffers"), "sniffing needs at least one sniffer; remove the sniffing section to turn it off")
	}
	if sniffing.Timeout < 0 {
		diagnostics.errorf(fieldLocation(loc, "timeout"), "timeout must not be negative")
	} else if sniffing.Timeout > 5*time.Second {
		diagnostics.warnf(fieldLocation(loc, "timeout"),
			"timeout %s delays every connection whose client does not speak first, such as SSH", sniffing.Timeout)
	}

	seen := make(map[string]int, len(sniffing.Sniffers))
	for index, sniffer := range sniffing.Sniffers {
		snifferLoc := indexLocation(fieldLocation(loc, "sniffers"), index)
		collectWarnings(diagnostics, sniffer, fieldLocation(snifferLoc, "settings"))

		name := sniffer.Info().Name
		if first, repeated := seen[name]; repeated {
			diagnostics.warnf(snifferLoc, "sniffer %q already appears at index %d", name, first)
			continue
		}
		seen[name] = index
	}
}

func checkOutbound(diagnostics *Diagnostics, outbound blueprint.Outbound, loc string) {
	protocolLoc := fieldLocation(loc, "protocol")

	protocolCount := 0
	for _, isSet := range []bool{outbound.Proxy != nil, outbound.Freedom != nil, outbound.Block} {
		if isSet {
			protocolCount++
		}
	}
	switch {
	case protocolCount == 0:
		diagnostics.errorf(protocolLoc, "an outbound needs a protocol: a proxy protocol, freedom or block")
		return
	case protocolCount > 1:
		diagnostics.errorf(protocolLoc, "an outbound has exactly one protocol")
		return
	}

	if outbound.Proxy != nil {
		checkProxyOutbound(diagnostics, outbound, loc)
		return
	}

	special := freedomProtocol
	if outbound.Block {
		special = blockProtocol
	}
	if outbound.Server.IsValid() {
		diagnostics.errorf(fieldLocation(loc, "server"), "%s reaches no proxy server, so it takes no server", special)
	}
	if !outbound.StreamTransport.IsZero() {
		diagnostics.errorf(fieldLocation(loc, "transport"), "%s takes no transport: its connections are always plain tcp and udp", special)
	}
	if outbound.HandshakeTimeout != 0 {
		diagnostics.errorf(fieldLocation(loc, "handshake_timeout"), "%s has no proxy handshake", special)
	}
	if outbound.Block && outbound.DialTimeout != 0 {
		diagnostics.errorf(fieldLocation(loc, "dial_timeout"), "block dials nothing")
	}

	if outbound.Freedom != nil {
		settingsLoc := fieldLocation(protocolLoc, "settings")
		collectWarnings(diagnostics, outbound.Freedom.StreamTransport.Base, fieldLocation(settingsLoc, "tcp"))
		collectWarnings(diagnostics, outbound.Freedom.DatagramTransport.Base, fieldLocation(settingsLoc, "udp"))
		if outbound.Freedom.NAT != nil {
			checkUDPPolicy(diagnostics, *outbound.Freedom.NAT, fieldLocation(settingsLoc, "nat"))
		}
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

// The same overlay twice is legal and occasionally deliberate -- TLS inside
// TLS is a real technique -- so this is a warning, not an error.
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

// The module knows what is worth warning about; only the caller knows where
// in the config file it came from.
func collectWarnings(diagnostics *Diagnostics, component spec.Config, loc string) {
	if component == nil {
		return
	}
	for _, warning := range component.Warnings() {
		diagnostics.warnf(loc, "%s", warning)
	}
}
