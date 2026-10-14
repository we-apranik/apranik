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

// MuxCoolTCP is TCP over Mux.Cool, which is not built: Check refuses every field
// except Enabled set to false.
type MuxCoolTCP struct {
	Enabled     *bool
	Concurrency *int
	MaxTotal    *int
}

type MuxCoolUDP struct {
	// Default true: UDP goes through Mux.Cool. False sends (or takes) the
	// protocol's plain UDP.
	Enabled *bool

	// A pool of main connections is not built: Check refuses it.
	Concurrency *int

	// Nil means the defaults.
	XUDP *MuxCoolXUDP
}

// MuxCoolXUDP: one sub-connection carries every destination of a UDP session.
type MuxCoolXUDP struct {
	// Default true. False would be plain Mux.Cool UDP, which is not built:
	// Check refuses it.
	Enabled *bool

	// Nil means the defaults.
	GlobalID *MuxCoolGlobalID
}

// MuxCoolGlobalID is the XUDP session ID that lets a client that reconnects get
// its UDP session back.
type MuxCoolGlobalID struct {
	// Default true: a client sends the ID, a server holds a session whose stream
	// was lost and gives it to the next stream with the same ID.
	Enabled *bool

	// How long a server holds a session after its stream was lost. Server only:
	// Check refuses it on an outbound. Nil means the default; a value must be
	// positive.
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

// UDPIsXUDP is whether UDP goes through Mux.Cool as XUDP. A nil MuxCool is the
// defaults.
func (m *MuxCool) UDPIsXUDP() bool {
	udp := m.udp()
	if udp == nil {
		return true
	}
	xudp := m.xudp()
	return isTrueOrLeftOut(udp.Enabled) && (xudp == nil || isTrueOrLeftOut(xudp.Enabled))
}

// Options resolves the defaults into what a protocol is told. A nil MuxCool is the
// defaults.
func (m *MuxCool) Options() spec.MuxCoolOptions {
	return spec.MuxCoolOptions{XUDP: m.UDPIsXUDP()}
}

// XUDPGlobalIDEnabled is whether the Global ID is used: XUDP is on and the ID is
// not turned off. A nil MuxCool is the defaults.
func (m *MuxCool) XUDPGlobalIDEnabled() bool {
	if !m.UDPIsXUDP() {
		return false
	}
	globalID := m.globalID()
	return globalID == nil || isTrueOrLeftOut(globalID.Enabled)
}

// XUDPGlobalIDHoldTime is how long a server holds a session whose stream was
// lost; zero when left out, and the consumer applies its default.
func (m *MuxCool) XUDPGlobalIDHoldTime() time.Duration {
	if globalID := m.globalID(); globalID != nil && globalID.HoldTime != nil {
		return *globalID.HoldTime
	}
	return 0
}
