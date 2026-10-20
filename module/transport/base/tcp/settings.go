package tcp

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/we-apranik/apranik/config/types"
)

type Settings struct {
	KeepAlive *types.Duration `json:"keep_alive"`

	// Between unanswered probes, and how many before the connection is
	// dropped, so a dead peer is found after about keep_alive +
	// keep_alive_interval * keep_alive_count. Absent uses Go's default (15s,
	// 9); a written zero is refused. PROVISIONAL: the defaults are Go's, not a
	// choice made here.
	KeepAliveInterval *types.Duration `json:"keep_alive_interval"`
	KeepAliveCount    *int            `json:"keep_alive_count"`

	DialTimeout *types.Duration `json:"dial_timeout"`

	// When a domain has several addresses, how long one connection attempt
	// runs before the next starts beside it (RFC 8305's Connection Attempt
	// Delay). Absent means defaultNextAddressDelay; a written zero is refused.
	NextAddressDelay *types.Duration `json:"next_address_delay"`

	NoDelay bool `json:"no_delay"`
}

func (s *Settings) Validate() error {
	if err := types.CheckPositive(s.DialTimeout); err != nil {
		return fmt.Errorf("tcp: dial_timeout %w", err)
	}
	if err := types.CheckPositive(s.NextAddressDelay); err != nil {
		return fmt.Errorf("tcp: next_address_delay %w", err)
	}
	if s.KeepAlive != nil && *s.KeepAlive == 0 {
		return errors.New("tcp: keep_alive must not be zero: leave it out for the default, or write a negative duration to disable probes")
	}
	if s.KeepAlive != nil && time.Duration(*s.KeepAlive) > 24*time.Hour {
		return fmt.Errorf("tcp: keep_alive %s is longer than a day, which is almost certainly a mistake", s.KeepAlive)
	}
	if err := types.CheckPositive(s.KeepAliveInterval); err != nil {
		return fmt.Errorf("tcp: keep_alive_interval %w", err)
	}
	if s.KeepAliveInterval != nil && time.Duration(*s.KeepAliveInterval) > 24*time.Hour {
		return fmt.Errorf("tcp: keep_alive_interval %s must be at most a day", s.KeepAliveInterval)
	}
	if err := types.CheckPositive(s.KeepAliveCount); err != nil {
		return fmt.Errorf("tcp: keep_alive_count %w", err)
	}
	if s.KeepAliveCount != nil && *s.KeepAliveCount > 255 {
		return fmt.Errorf("tcp: keep_alive_count %d must be between 1 and 255", *s.KeepAliveCount)
	}
	return nil
}

// PROVISIONAL.
const defaultDialTimeout = 10 * time.Second

// PROVISIONAL: the delay RFC 8305 recommends.
const defaultNextAddressDelay = 250 * time.Millisecond

func (s *Settings) nextAddressDelay() time.Duration {
	return time.Duration(types.ValueOrDefault(s.NextAddressDelay, types.Duration(defaultNextAddressDelay)))
}

func (s *Settings) dialTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.DialTimeout, types.Duration(defaultDialTimeout)))
}

func (s *Settings) keepAliveConfig() net.KeepAliveConfig {
	if s.KeepAlive != nil && *s.KeepAlive < 0 {
		return net.KeepAliveConfig{Enable: false}
	}
	return net.KeepAliveConfig{
		Enable:   true,
		Idle:     time.Duration(types.ValueOrDefault(s.KeepAlive, 0)),
		Interval: time.Duration(types.ValueOrDefault(s.KeepAliveInterval, 0)),
		Count:    types.ValueOrDefault(s.KeepAliveCount, 0),
	}
}

func (s *Settings) Warnings() []string { return nil }
