package inbound

import (
	"errors"

	"github.com/we-apranik/apranik/core/stats"
)

func refusalReasonFor(err error, stage stats.RefusalReason) stats.RefusalReason {
	if isTimeout(err) {
		return stats.RefusalHandshakeTimedOut
	}
	return stage
}

func isTimeout(err error) bool {
	var timeoutError interface{ Timeout() bool }
	return errors.As(err, &timeoutError) && timeoutError.Timeout()
}
