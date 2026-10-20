package config

import (
	"time"

	"github.com/we-apranik/apranik/config/types"
)

func durationOrNil(written *types.Duration) *time.Duration {
	if written == nil {
		return nil
	}
	duration := time.Duration(*written)
	return &duration
}
