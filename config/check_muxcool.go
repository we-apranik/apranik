package config

import (
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/spec"
)

// An outbound's muxcool key. The whole shape is known; what is not
// built is refused when it is written, so a setting is never silently ignored.
func checkMuxCool(diagnostics *Diagnostics, outbound blueprint.Outbound, loc string) {
	muxCool := outbound.MuxCool
	if muxCool == nil {
		return
	}
	muxCoolLoc := fieldLocation(loc, "muxcool")

	if outbound.Proxy == nil {
		diagnostics.errorf(muxCoolLoc, "muxcool is for a proxy outbound; freedom and groups reach no proxy server")
		return
	}
	if _, ok := outbound.Proxy.(spec.MuxCoolConfigurable); !ok {
		diagnostics.errorf(muxCoolLoc, "proxy %q cannot carry muxcool", outbound.Proxy.Info().Name)
		return
	}

	if tcp := muxCool.TCP; tcp != nil {
		tcpLoc := fieldLocation(muxCoolLoc, "tcp")
		if tcp.Enabled != nil && *tcp.Enabled {
			diagnostics.errorf(fieldLocation(tcpLoc, "enabled"), "TCP over Mux.Cool is not built yet; leave muxcool.tcp out, or set enabled to false")
		}
		if tcp.Concurrency != nil {
			diagnostics.errorf(fieldLocation(tcpLoc, "concurrency"), "TCP over Mux.Cool is not built yet, so concurrency has no effect")
		}
		if tcp.MaxTotal != nil {
			diagnostics.errorf(fieldLocation(tcpLoc, "max_total"), "TCP over Mux.Cool is not built yet, so max_total has no effect")
		}
	}

	if udp := muxCool.UDP; udp != nil {
		udpLoc := fieldLocation(muxCoolLoc, "udp")
		if udp.Concurrency != nil {
			diagnostics.errorf(fieldLocation(udpLoc, "concurrency"),
				"sharing a main connection between UDP sessions is not built yet; each UDP session has its own, so concurrency has no effect")
		}
		if udp.XUDP != nil && !*udp.XUDP && (udp.Enabled == nil || *udp.Enabled) {
			diagnostics.errorf(fieldLocation(udpLoc, "xudp"),
				"plain Mux.Cool UDP is not built yet; to send plain UDP of the protocol, set muxcool.udp.enabled to false")
		}
	}
}
