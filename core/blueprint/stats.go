package blueprint

import "time"

// Stats says what the traffic statistics count. The zero value counts every
// inbound, outbound and user, which is the default.
type Stats struct {
	Disabled bool

	// A list narrows counting to the tags in it; an empty list means all of
	// that kind.
	Inbounds  []string
	Outbounds []string

	Tracking StatsTracking

	// Whether each outbound's dials are timed. Off unless asked: the handler
	// reads the clock around every dial for it.
	DialHealth bool
}

// StatsTracking is the live list: the TCP connections and UDP destinations
// open now, so a reader can list and close them and show the users who are
// online. A nil field is absent from the config and means its default.
type StatsTracking struct {
	// Nil means the default: see Blueprint.IsLiveTrackingEnabled.
	Enabled *bool

	// A UDP entry (one destination of a UDP session) leaves the list after
	// this much silence in either direction. Nil means
	// udp.DefaultLiveTrackingUDPEntryIdleTimeout; a value must be positive.
	UDPEntryIdleTimeout *time.Duration

	// How many entries one UDP session lists at once. Nil means
	// udp.DefaultLiveTrackingUDPMaxEntriesPerSession; a value must be positive.
	UDPMaxEntriesPerSession *int
}

// IsLiveTrackingEnabled says whether live conversations are tracked: what
// stats.tracking.enabled says, else on exactly when an API is configured to
// read them. Disabled counting tracks nothing.
func (b *Blueprint) IsLiveTrackingEnabled() bool {
	if b.Stats != nil && b.Stats.Disabled {
		return false
	}
	if b.Stats != nil && b.Stats.Tracking.Enabled != nil {
		return *b.Stats.Tracking.Enabled
	}
	return len(b.APIs) > 0
}
