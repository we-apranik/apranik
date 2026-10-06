package blueprint

// Stats says what the traffic statistics count. The zero value counts every
// inbound, outbound and user, which is the default.
type Stats struct {
	Disabled bool

	// A list narrows counting to the names in it; an empty list means all of
	// that kind. Users cannot be checked against anything: core learns them
	// only when a protocol authenticates one.
	Inbounds  []string
	Outbounds []string
	Users     []string

	// Whether live connections (TCP connections and UDP destinations) are
	// listed, so a reader can show them and the users who are online. Nil means the default, which is on exactly
	// when an API is configured to read them; no API exists yet, so it is off.
	LiveConnections *bool

	// Whether each outbound's dials are timed. Off unless asked: the handler
	// reads the clock around every dial for it.
	DialHealth bool
}
