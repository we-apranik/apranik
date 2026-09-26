package tcp

import (
	"fmt"
	"net"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
)

// No listen address here: a listener is handed its bind address, so the field
// would be meaningless on an outbound.
type Settings struct {
	// How long a connection is idle before the first probe. Zero uses Go's
	// default; negative disables probes.
	KeepAlive types.Duration `json:"keep_alive"`

	// Between unanswered probes, and how many before the connection is
	// dropped, so a dead peer is found after about keep_alive +
	// keep_alive_interval * keep_alive_count. Zero uses Go's default (15s,
	// 9). PROVISIONAL: the defaults are Go's, not a choice made here.
	KeepAliveInterval types.Duration `json:"keep_alive_interval"`
	KeepAliveCount    int            `json:"keep_alive_count"`

	// Disables Nagle's algorithm. On by default: a proxy carries interactive
	// traffic, where coalescing small writes shows up as lag.
	NoDelay bool `json:"no_delay"`
}

func (s *Settings) Validate() error {
	if time.Duration(s.KeepAlive) > 24*time.Hour {
		return fmt.Errorf("tcp: keep_alive %s is longer than a day, which is almost certainly a mistake", s.KeepAlive)
	}
	if s.KeepAliveInterval < 0 || time.Duration(s.KeepAliveInterval) > 24*time.Hour {
		return fmt.Errorf("tcp: keep_alive_interval %s must be between zero and a day", s.KeepAliveInterval)
	}
	if s.KeepAliveCount < 0 || s.KeepAliveCount > 255 {
		return fmt.Errorf("tcp: keep_alive_count %d must be between 0 and 255", s.KeepAliveCount)
	}
	return nil
}

func (s *Settings) keepAliveConfig() net.KeepAliveConfig {
	if s.KeepAlive < 0 {
		return net.KeepAliveConfig{Enable: false}
	}
	return net.KeepAliveConfig{
		Enable:   true,
		Idle:     time.Duration(s.KeepAlive),
		Interval: time.Duration(s.KeepAliveInterval),
		Count:    s.KeepAliveCount,
	}
}

func (s *Settings) Warnings() []string { return nil }
