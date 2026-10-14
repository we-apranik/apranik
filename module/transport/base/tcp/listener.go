package tcp

import (
	"context"
	"fmt"
	"net"

	"github.com/we-apranik/apranik/spec"
)

type streamListener struct {
	listener  *net.TCPListener
	keepAlive net.KeepAliveConfig
	noDelay   bool
}

// The accept syscall takes no context, so ctx is only checked before
// blocking; Close is the real way to stop it.
func (l *streamListener) AcceptStream(ctx context.Context) (spec.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	connection, err := l.listener.AcceptTCP()
	if err != nil {
		return nil, fmt.Errorf("tcp: accept: %w", err)
	}

	// The same settings the dialer applies, so both directions of a relay
	// behave alike.
	connection.SetNoDelay(l.noDelay)
	if l.keepAlive.Enable {
		connection.SetKeepAliveConfig(l.keepAlive)
	} else {
		connection.SetKeepAlive(false)
	}

	return connection, nil
}

func (l *streamListener) LocalAddr() net.Addr { return l.listener.Addr() }

func (l *streamListener) Close() error { return l.listener.Close() }
