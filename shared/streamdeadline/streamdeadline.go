// Package streamdeadline bounds a handshake by its context's deadline: proxy
// protocols read and write the stream directly, so the stream's own deadline is
// the only way their blocking calls obey the context.
package streamdeadline

import (
	"context"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// ApplyContextDeadline sets the stream's deadline to the context's, if it has
// one, and returns the function that clears it again so the relay is not cut off.
// Use it as: defer streamdeadline.ApplyContextDeadline(ctx, stream)()
func ApplyContextDeadline(ctx context.Context, stream spec.Stream) (clearDeadline func()) {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		return func() {}
	}
	stream.SetDeadline(deadline)
	return func() { stream.SetDeadline(time.Time{}) }
}
