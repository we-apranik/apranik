package config

import (
	"time"

	"github.com/we-apranik/apranik/config/types"
)

// durationOrNil keeps a field that was not written absent.
func durationOrNil(written *types.Duration) *time.Duration {
	if written == nil {
		return nil
	}
	duration := time.Duration(*written)
	return &duration
}
