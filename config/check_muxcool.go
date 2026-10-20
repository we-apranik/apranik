package config

import (
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/spec"
)

type muxCoolRole int

const (
	muxCoolClient muxCoolRole = iota
	muxCoolServer
)

func checkMuxCool(diagnostics *Diagnostics, outbound blueprint.Outbound, loc string) {
	if outbound.MuxCool == nil {
		return
	}
	muxCoolLoc := fieldLocation(loc, "muxcool")
	if outbound.Proxy == nil {
		diagnostics.errorf(muxCoolLoc, "muxcool is for a proxy outbound; freedom and groups reach no proxy server")
		return
	}
	checkMuxCoolOfProxy(diagnostics, outbound.MuxCool, outbound.Proxy, muxCoolClient, muxCoolLoc)
}

func checkInboundMuxCool(diagnostics *Diagnostics, inbound blueprint.Inbound, loc string) {
	if inbound.MuxCool == nil {
		return
	}
	muxCoolLoc := fieldLocation(loc, "muxcool")
	if inbound.StreamProxy == nil {
		diagnostics.errorf(muxCoolLoc, "muxcool is for an inbound with a proxy protocol; mixed and a udp inbound take none")
		return
	}
	checkMuxCoolOfProxy(diagnostics, inbound.MuxCool, inbound.StreamProxy, muxCoolServer, muxCoolLoc)
}

func checkMuxCoolOfProxy(diagnostics *Diagnostics, muxCool *blueprint.MuxCool, proxy spec.StreamProxyModule, role muxCoolRole, muxCoolLoc string) {
	if _, ok := proxy.(spec.MuxCoolConfigurable); !ok {
		diagnostics.errorf(muxCoolLoc, "proxy %q cannot carry muxcool", proxy.Info().Name)
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
		checkMuxCoolUDP(diagnostics, muxCool, role, fieldLocation(muxCoolLoc, "udp"))
	}
}

func checkMuxCoolUDP(diagnostics *Diagnostics, muxCool *blueprint.MuxCool, role muxCoolRole, udpLoc string) {
	udp := muxCool.UDP
	udpEnabled := udp.Enabled == nil || *udp.Enabled
	if udp.Concurrency != nil {
		diagnostics.errorf(fieldLocation(udpLoc, "concurrency"),
			"sharing a main connection between UDP sessions is not built yet; each UDP session has its own, so concurrency has no effect")
	}
	xudp := udp.XUDP
	if xudp == nil {
		return
	}
	xudpLoc := fieldLocation(udpLoc, "xudp")

	if xudp.Enabled != nil && !*xudp.Enabled && udpEnabled {
		verb := "send"
		if role == muxCoolServer {
			verb = "take"
		}
		diagnostics.errorf(fieldLocation(xudpLoc, "enabled"),
			"plain Mux.Cool UDP is not built yet; to %s plain UDP of the protocol, set muxcool.udp.enabled to false", verb)
	}
	if xudp.GlobalID != nil {
		checkMuxCoolGlobalID(diagnostics, xudp.GlobalID, muxCool.UDPIsXUDP(), role, fieldLocation(xudpLoc, "global_id"))
	}
}

func checkMuxCoolGlobalID(diagnostics *Diagnostics, globalID *blueprint.MuxCoolGlobalID, xudpIsOn bool, role muxCoolRole, globalIDLoc string) {
	if !xudpIsOn {
		diagnostics.warnf(globalIDLoc, "global_id has no effect while XUDP is off")
	}
	if globalID.HoldTime == nil {
		return
	}
	holdTimeLoc := fieldLocation(globalIDLoc, "hold_time")
	switch {
	case role == muxCoolClient:
		diagnostics.errorf(holdTimeLoc, "hold_time is for a server: only a server holds a session after its stream was lost; leave it out on an outbound")
	case *globalID.HoldTime <= 0:
		diagnostics.errorf(holdTimeLoc, "must be positive; leave it out for the default")
	case globalID.Enabled != nil && !*globalID.Enabled:
		diagnostics.warnf(holdTimeLoc, "hold_time has no effect while global_id.enabled is false")
	}
}
