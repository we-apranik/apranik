package inbound

import (
	"errors"

	"github.com/The-Apranik/Apranik/core/stats"
)

// A deadline is a client that was too slow, in whichever stage it happened;
// anything else is the stage's own reason.
func refusalReasonFor(err error, stage stats.RefusalReason) stats.RefusalReason {
	if isTimeout(err) {
		return stats.RefusalHandshakeTimedOut
	}
	return stage
}

// Covers context.DeadlineExceeded and os.ErrDeadlineExceeded, which a read
// deadline produces, and a net.Error that reports a timeout.
func isTimeout(err error) bool {
	var timeoutError interface{ Timeout() bool }
	return errors.As(err, &timeoutError) && timeoutError.Timeout()
}
