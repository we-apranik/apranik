package blueprint

import "time"

type Stats struct {
	Disabled bool

	Inbounds  []string
	Outbounds []string

	Tracking StatsTracking

	// Whether each outbound's dials are timed. Off unless asked: the handler
	// reads the clock around every dial for it.
	DialHealth bool
}

type StatsTracking struct {
	Enabled *bool

	UDPEntryIdleTimeout *time.Duration

	UDPMaxEntriesPerSession *int
}

func (b *Blueprint) IsLiveTrackingEnabled() bool {
	if b.Stats != nil && b.Stats.Disabled {
		return false
	}
	if b.Stats != nil && b.Stats.Tracking.Enabled != nil {
		return *b.Stats.Tracking.Enabled
	}
	return len(b.APIs) > 0
}
