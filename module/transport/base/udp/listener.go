package udp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	"github.com/The-Apranik/Apranik/spec"
)

// Nothing is open until ListenDatagram: binding is the caller's moment to
// choose, not this type's. A caller that wants a port conflict reported at
// startup calls it then; the SOCKS5 associate path calls it per association.
type datagramListener struct {
	bind     spec.Endpoint
	resolver spec.Resolver

	mu     sync.Mutex
	socket *socket
}

func newDatagramListener(bind spec.Endpoint, resolver spec.Resolver) (*datagramListener, error) {
	// A zero one is refused rather than becoming a wildcard: an inbound that
	// bound every interface because a field was omitted is a real exposure.
	if !bind.IsValid() {
		return nil, errors.New("udp: bind address has no IP")
	}
	return &datagramListener{bind: bind, resolver: resolver}, nil
}

// Port 0 in the bind address means "any free port". One datagram per listener:
// they would share the one connection, so the second caller's Close would take
// the first caller's datagram down with it.
func (l *datagramListener) ListenDatagram(ctx context.Context) (spec.Datagram, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.socket != nil {
		return nil, fmt.Errorf("udp: listener on %s has already handed out its datagram", l.bind)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("udp: listen %s: %w", l.bind, err)
	}

	address := netip.AddrPortFrom(l.bind.Addr(), l.bind.Port()).String()
	var config net.ListenConfig
	connection, err := config.ListenPacket(ctx, "udp", address)
	if err != nil {
		return nil, fmt.Errorf("udp: listen %s: %w", address, err)
	}

	udpConnection, ok := connection.(*net.UDPConn)
	if !ok {
		connection.Close()
		return nil, fmt.Errorf("udp: listen %s: got %T, want *net.UDPConn", address, connection)
	}

	l.socket = newSocket(udpConnection, "udp", l.resolver)
	return l.socket, nil
}

// Zero until ListenDatagram has bound, since there is no address before that.
func (l *datagramListener) LocalAddr() net.Addr {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.socket == nil {
		return nil
	}
	return l.socket.LocalAddr()
}

func (l *datagramListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.socket == nil {
		return nil
	}
	return l.socket.Close()
}
