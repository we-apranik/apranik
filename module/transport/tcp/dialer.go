package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	"github.com/The-Apranik/Apranik/spec"
)

type streamDialer struct {
	dialer  net.Dialer
	noDelay bool
}

// *net.TCPConn is returned unwrapped: it's already a spec.Stream, since
// net.Conn carries everything but CloseWrite and TCPConn has that.
func (d *streamDialer) DialStream(ctx context.Context, destination spec.Destination) (spec.Stream, error) {
	target, err := dialTarget(destination)
	if err != nil {
		return nil, err
	}

	connection, err := d.dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("tcp: dial %s: %w", target, err)
	}

	tcpConnection, ok := connection.(*net.TCPConn)
	if !ok {
		connection.Close()
		return nil, fmt.Errorf("tcp: dial %s: got %T, want *net.TCPConn", target, connection)
	}

	tcpConnection.SetNoDelay(d.noDelay)
	return tcpConnection, nil
}

// Holds nothing of its own: every Stream it returns owns its socket.
func (d *streamDialer) Close() error { return nil }

// This is the only layer that resolves, and only its own target -- a domain
// passes through unresolved.
func dialTarget(destination spec.Destination) (string, error) {
	if !destination.IsValid() {
		return "", errors.New("tcp: no destination")
	}
	if destination.Port() == 0 {
		return "", fmt.Errorf("tcp: address %s has no port", destination.Host())
	}
	return net.JoinHostPort(destination.Host(), strconv.Itoa(int(destination.Port()))), nil
}

// A bind address may carry port 0, meaning "any free port" -- that is how a
// SOCKS5 associate socket is opened -- so it does not go through dialTarget.
// A zero one is refused rather than becoming a wildcard: an inbound that bound
// every interface because a field was omitted is a real exposure.
func bindTarget(bind spec.Endpoint) (string, error) {
	if !bind.IsValid() {
		return "", errors.New("tcp: bind address has no IP")
	}
	return netip.AddrPortFrom(bind.Addr(), bind.Port()).String(), nil
}
