// Package outbound is where traffic leaves Apranik: through a proxy server, or
// straight to the destination with freedom. Blocking is a routing action, not
// an outbound. Each outbound states in advance
// which networks it can carry, so nothing is routed to it that it cannot serve.
package outbound

import (
	"context"
	"time"
)

// A server that is blocked often swallows packets rather than refusing, so
// without a bound a dial waits for the operating system's own limit.
// PROVISIONAL: 10 seconds.
const DefaultDialTimeout = 10 * time.Second

// Bounds the proxy handshake with the server once the server stream is up.
// PROVISIONAL: 10 seconds.
const DefaultHandshakeTimeout = 10 * time.Second

// Zero means the default; negative means no bound.
func boundedContext(ctx context.Context, timeout, defaultTimeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
