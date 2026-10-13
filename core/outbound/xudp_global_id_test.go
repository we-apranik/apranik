package outbound

import (
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/spec"
)

func endpointOf(t *testing.T, text string) spec.Endpoint {
	t.Helper()
	endpoint, err := spec.NewEndpoint(netip.MustParseAddrPort(text).Addr(), netip.MustParseAddrPort(text).Port())
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func TestTheSameSourceGivesTheSameGlobalIDAndAnotherSourceAnotherOne(t *testing.T) {
	deriver := NewXUDPGlobalIDDeriverWithKey([32]byte{1})
	first := deriver.For(endpointOf(t, "192.0.2.7:50000"))

	if first == ([8]byte{}) {
		t.Fatal("a session with a source got no Global ID")
	}
	if again := deriver.For(endpointOf(t, "192.0.2.7:50000")); again != first {
		t.Errorf("the same source gave %v and then %v", first, again)
	}
	for _, other := range []string{"192.0.2.7:50001", "192.0.2.8:50000", "[2001:db8::7]:50000"} {
		if got := deriver.For(endpointOf(t, other)); got == first {
			t.Errorf("%s gave the same ID as 192.0.2.7:50000", other)
		}
	}
}

func TestADifferentKeyGivesADifferentGlobalIDSoAnAddressAloneRevealsNothing(t *testing.T) {
	source := endpointOf(t, "192.0.2.7:50000")
	one := NewXUDPGlobalIDDeriverWithKey([32]byte{1}).For(source)
	another := NewXUDPGlobalIDDeriverWithKey([32]byte{2}).For(source)
	if one == another {
		t.Error("two keys gave the same ID for one source")
	}
}

func TestEachDeriverMakesItsOwnRandomKey(t *testing.T) {
	first, err := NewXUDPGlobalIDDeriver()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewXUDPGlobalIDDeriver()
	if err != nil {
		t.Fatal(err)
	}
	source := endpointOf(t, "192.0.2.7:50000")
	if first.For(source) == second.For(source) {
		t.Error("two random keys gave the same ID for one source")
	}
}

func TestASessionWithNoSourceGetsNoGlobalID(t *testing.T) {
	if got := NewXUDPGlobalIDDeriverWithKey([32]byte{1}).For(spec.Endpoint{}); got != ([8]byte{}) {
		t.Errorf("no source gave %v, want none", got)
	}
}
