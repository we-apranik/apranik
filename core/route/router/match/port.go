package match

// PortRange is inclusive at both ends; a single port has First == Last.
type PortRange struct {
	First, Last uint16
}

// PortMatcher is a short list scanned in order: rules name a handful of ports.
type PortMatcher struct {
	ranges []PortRange
}

func NewPortMatcher(ranges []PortRange) *PortMatcher {
	return &PortMatcher{ranges: ranges}
}

func (m *PortMatcher) Contains(port uint16) bool {
	for _, portRange := range m.ranges {
		if port >= portRange.First && port <= portRange.Last {
			return true
		}
	}
	return false
}
