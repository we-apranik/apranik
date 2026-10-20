package registry

import "testing"

func TestEveryKindHasADescription(t *testing.T) {
	if len(kindDescriptions) != int(KindAPI)+1 {
		t.Errorf("kindDescriptions has %d rows for %d kinds; KindAPI must stay the last kind", len(kindDescriptions), int(KindAPI)+1)
	}
	for kind := KindBaseStream; kind <= KindAPI; kind++ {
		if kind.String() == "" || kindDescriptions[kind].name == "" {
			t.Errorf("kind %d has no name", int(kind))
		}
	}
}

func TestAnUnknownKindIsNamedByItsNumber(t *testing.T) {
	if got := Kind(99).String(); got != "unknown kind 99" {
		t.Errorf("String = %q", got)
	}
	if got := Kind(-1).String(); got != "unknown kind -1" {
		t.Errorf("String = %q", got)
	}
}
