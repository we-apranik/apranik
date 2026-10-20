package match

type PortRange struct {
	First, Last uint16
}

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
