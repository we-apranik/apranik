package muxcool

import (
	"fmt"
	"strconv"
)

type address struct {
	network string
	text    string
}

func (a address) Network() string { return a.network }
func (a address) String() string  { return a.text }

func subConnectionAddress(id uint16) address {
	return address{network: "muxcool", text: "sub-connection " + strconv.Itoa(int(id))}
}

func destinationAddress(network Network, text string) address {
	switch network {
	case NetworkTCP:
		return address{network: "tcp", text: text}
	case NetworkUDP:
		return address{network: "udp", text: text}
	default:
		return address{network: "muxcool", text: fmt.Sprintf("%s %s", network, text)}
	}
}
