package muxcool

type Networks uint8

const (
	NetworksTCP Networks = 1 << iota
	NetworksUDP
)

func (n Networks) accepts(network Network) bool {
	if n == 0 {
		return true
	}
	switch network {
	case NetworkTCP:
		return n&NetworksTCP != 0
	case NetworkUDP:
		return n&NetworksUDP != 0
	default:
		return false
	}
}
