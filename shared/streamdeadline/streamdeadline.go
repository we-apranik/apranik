// Package streamdeadline bounds a handshake by its context: handshakes read
// and write the stream directly, so the stream's own deadline is the only way
// their blocking calls obey the context.
package streamdeadline

import (
	"context"
	"sync"
	"time"

	"github.com/we-apranik/apranik/spec"
)

// BindToContext makes blocking reads and writes on the stream fail once ctx
// ends: at its deadline, or as soon as it is cancelled.
//
// release must be called before the stream is handed on. It clears the
// deadline, so the stream is usable again, and returns ctx's error if ctx
// ended first: the handshake may have been cut short and should be abandoned.
// Callers that only need the deadline cleared may defer it and ignore the
// error.
func BindToContext(ctx context.Context, stream spec.Stream) (release func() error) {
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		stream.SetDeadline(deadline)
	}

	var releaseMutex sync.Mutex
	released := false
	stopInterrupting := context.AfterFunc(ctx, func() {
		releaseMutex.Lock()
		defer releaseMutex.Unlock()
		if !released {
			stream.SetDeadline(time.Unix(1, 0))
		}
	})

	return func() error {
		releaseMutex.Lock()
		defer releaseMutex.Unlock()
		released = true
		interrupted := !stopInterrupting()
		stream.SetDeadline(time.Time{})
		if interrupted {
			return ctx.Err()
		}
		return nil
	}
}
