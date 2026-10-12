package udp

// Addresses are parameters, not settings: a listener is handed its bind
// address, and a dialer whichever local address the caller wants. Resolving is
// the resolver's, so its timeout and cache are configured in the
// dns section, not here.
type Settings struct{}

func (s *Settings) Validate() error { return nil }

func (s *Settings) Warnings() []string { return nil }
