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

	// How long a dial may take, name resolution included. Zero means
	// defaultDialTimeout.
	DialTimeout types.Duration `json:"dial_timeout"`

	// When a domain has several addresses, how long one connection attempt
	// runs before the next starts beside it (RFC 8305's Connection Attempt
	// Delay). Zero means defaultNextAddressDelay.
	NextAddressDelay types.Duration `json:"next_address_delay"`

	// Disables Nagle's algorithm. On by default: a proxy carries interactive
	// traffic, where coalescing small writes shows up as lag.
	NoDelay bool `json:"no_delay"`
}

func (s *Settings) Validate() error {
	if s.DialTimeout < 0 {
		return fmt.Errorf("tcp: dial_timeout %s must not be negative", s.DialTimeout)
	}
	if s.NextAddressDelay < 0 {
		return fmt.Errorf("tcp: next_address_delay %s must not be negative", s.NextAddressDelay)
	}
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

// PROVISIONAL.
const defaultDialTimeout = 10 * time.Second

// PROVISIONAL: the delay RFC 8305 recommends.
const defaultNextAddressDelay = 250 * time.Millisecond

func (s *Settings) nextAddressDelay() time.Duration {
	if s.NextAddressDelay == 0 {
		return defaultNextAddressDelay
	}
	return time.Duration(s.NextAddressDelay)
}

func (s *Settings) dialTimeout() time.Duration {
	if s.DialTimeout == 0 {
		return defaultDialTimeout
	}
	return time.Duration(s.DialTimeout)
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
