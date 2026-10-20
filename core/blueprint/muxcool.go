package blueprint

import (
	"time"

	"github.com/we-apranik/apranik/spec"
)

// MuxCool is the muxcool key of an outbound (what its client does) or of an
// inbound (what its server accepts); the shape is the same and each key means
// "use" on a client and "accept" on a server. Every field is optional, and a
// field left out takes its default, so a key left out sends and takes UDP as
// XUDP, like the clients and servers of Xray and sing-box, and multiplexes no
// TCP. Core reads it; a protocol sees only the options it makes.
type MuxCool struct {
	TCP *MuxCoolTCP
	UDP *MuxCoolUDP
}

type MuxCoolTCP struct {
	Enabled     *bool
	Concurrency *int
	MaxTotal    *int
}

type MuxCoolUDP struct {
	Enabled *bool

	Concurrency *int

	XUDP *MuxCoolXUDP
}

type MuxCoolXUDP struct {
	Enabled *bool

	GlobalID *MuxCoolGlobalID
}

type MuxCoolGlobalID struct {
	Enabled *bool

	HoldTime *time.Duration
}

func (m *MuxCool) udp() *MuxCoolUDP {
	if m == nil {
		return nil
	}
	return m.UDP
}

func (m *MuxCool) xudp() *MuxCoolXUDP {
	if udp := m.udp(); udp != nil {
		return udp.XUDP
	}
	return nil
}

func (m *MuxCool) globalID() *MuxCoolGlobalID {
	if xudp := m.xudp(); xudp != nil {
		return xudp.GlobalID
	}
	return nil
}

func isTrueOrLeftOut(setting *bool) bool { return setting == nil || *setting }

func (m *MuxCool) UDPIsXUDP() bool {
	udp := m.udp()
	if udp == nil {
		return true
	}
	xudp := m.xudp()
	return isTrueOrLeftOut(udp.Enabled) && (xudp == nil || isTrueOrLeftOut(xudp.Enabled))
}

func (m *MuxCool) Options() spec.MuxCoolOptions {
	return spec.MuxCoolOptions{XUDP: m.UDPIsXUDP()}
}

func (m *MuxCool) XUDPGlobalIDEnabled() bool {
	if !m.UDPIsXUDP() {
		return false
	}
	globalID := m.globalID()
	return globalID == nil || isTrueOrLeftOut(globalID.Enabled)
}

func (m *MuxCool) XUDPGlobalIDHoldTime() time.Duration {
	if globalID := m.globalID(); globalID != nil && globalID.HoldTime != nil {
		return *globalID.HoldTime
	}
	return 0
}
