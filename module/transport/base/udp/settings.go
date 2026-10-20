package udp

type Settings struct{}

func (s *Settings) Validate() error { return nil }

func (s *Settings) Warnings() []string { return nil }
