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

func (l *streamListener) AcceptStream(ctx context.Context) (spec.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	connection, err := l.listener.AcceptTCP()
	if err != nil {
		return nil, fmt.Errorf("tcp: accept: %w", err)
	}

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
