package blueprint

import "github.com/The-Apranik/Apranik/spec"

// MuxCool is an outbound's muxcool key. Every field is optional, and a field
// left out takes its default, so an outbound with no muxcool at all sends UDP
// as XUDP, like the clients of Xray and sing-box, and multiplexes no TCP.
// Core reads it; a protocol sees only the options it makes.
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
	// Default true: UDP goes through Mux.Cool. False sends the protocol's plain UDP.
	Enabled *bool

	// Default true: one sub-connection carries every destination of a UDP
	// session. False would be plain Mux.Cool UDP, which is not built: Check
	// refuses it.
	XUDP *bool

	// A pool of main connections is not built: Check refuses it.
	Concurrency *int
}

// Options resolves the defaults into what a protocol is told. A nil MuxCool is the
// defaults.
func (m *MuxCool) Options() spec.MuxCoolOptions {
	if m == nil || m.UDP == nil {
		return spec.MuxCoolOptions{XUDP: true}
	}
	enabled := m.UDP.Enabled == nil || *m.UDP.Enabled
	xudp := m.UDP.XUDP == nil || *m.UDP.XUDP
	return spec.MuxCoolOptions{XUDP: enabled && xudp}
}
