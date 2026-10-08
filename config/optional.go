package config

import (
	"time"

	"github.com/The-Apranik/Apranik/config/types"
)

// durationOrNil keeps a field that was not written absent.
func durationOrNil(written *types.Duration) *time.Duration {
	if written == nil {
		return nil
	}
	duration := time.Duration(*written)
	return &duration
}
