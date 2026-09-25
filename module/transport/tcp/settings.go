package tcp

import (
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
)

// No listen address here: a listener is handed its bind address, so the field
// would be meaningless on an outbound.
type Settings struct {
	// Zero uses the operating system's default; negative disables probes.
	KeepAlive types.Duration `json:"keep_alive"`

	// Disables Nagle's algorithm. On by default: a proxy carries interactive
	// traffic, where coalescing small writes shows up as lag.
	NoDelay bool `json:"no_delay"`
}

func (s *Settings) Validate() error {
	if time.Duration(s.KeepAlive) > 24*time.Hour {
		return fmt.Errorf("tcp: keep_alive %s is longer than a day, which is almost certainly a mistake", s.KeepAlive)
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }
