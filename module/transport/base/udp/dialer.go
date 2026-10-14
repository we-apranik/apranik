package udp

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/we-apranik/apranik/spec"
)

type datagramDialer struct{ resolver spec.Resolver }

// One socket per call: every caller needs its own ephemeral port.
// A zero localAddr, which is most of the time, takes any address and any free
// port, and leaves the socket dual-stack.
func (d datagramDialer) CreateDatagram(ctx context.Context, localAddr spec.Endpoint) (spec.Datagram, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("udp: open socket: %w", err)
	}

	network, source := localAddrSource(localAddr)

	var config net.ListenConfig
	connection, err := config.ListenPacket(ctx, network, source)
	if err != nil {
		return nil, fmt.Errorf("udp: open socket: %w", err)
	}

	udpConnection, ok := connection.(*net.UDPConn)
	if !ok {
		connection.Close()
		return nil, fmt.Errorf("udp: open socket: got %T, want *net.UDPConn", connection)
	}
	return newSocket(udpConnection, network, d.resolver), nil
}

// Holds nothing of its own: each Datagram owns the socket it was given.
func (datagramDialer) Close() error { return nil }

// Unlike a listener's bind address, a zero one here is not refused: an
// outbound socket has no address to advertise and every interface is a fine
// source, whereas an inbound that listened on every interface by omission
// would be a real exposure.
//
// An address narrows the socket to its own family; without one it stays
// dual-stack, so a single datagram reaches both IPv4 and IPv6 peers.
func localAddrSource(localAddr spec.Endpoint) (string, string) {
	if !localAddr.IsValid() {
		if localAddr.Port() == 0 {
			return "udp", ""
		}
		return "udp", net.JoinHostPort("", fmt.Sprint(localAddr.Port()))
	}

	network := "udp6"
	if localAddr.Addr().Is4() {
		network = "udp4"
	}
	return network, netip.AddrPortFrom(localAddr.Addr(), localAddr.Port()).String()
}
