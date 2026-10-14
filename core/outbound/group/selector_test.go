package group_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/outbound/group"
	"github.com/we-apranik/apranik/spec"
)

func newTestSelector(t *testing.T, members ...*scriptedMember) component.Outbound {
	t.Helper()
	outbounds := make([]component.Outbound, len(members))
	for index, member := range members {
		outbounds[index] = member
	}
	selector, err := group.NewSelector(group.SelectorConfig{Tag: "proxy", Members: outbounds})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { selector.Close() })
	return selector
}

func selectable(t *testing.T, selector component.Outbound) component.SelectableOutbound {
	t.Helper()
	selectable, ok := selector.(component.SelectableOutbound)
	if !ok {
		t.Fatal("a selector is not a SelectableOutbound")
	}
	return selectable
}

func TestASelectorStartsOnItsFirstMemberAndReportsIt(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	selector := newTestSelector(t, first, second)

	if used := memberUsed(t, selector, first, second); used != "first" {
		t.Errorf("used %q at the start, want the first member", used)
	}
	reporter := selector.(component.GroupStateReporter)
	if tags := reporter.MemberTags(); len(tags) != 2 || tags[0] != "first" || tags[1] != "second" {
		t.Errorf("MemberTags = %v", tags)
	}
	if reporter.CurrentMemberTag() != "first" {
		t.Errorf("CurrentMemberTag = %q, want first", reporter.CurrentMemberTag())
	}
	if _, isProbing := selector.(component.ProbingOutbound); isProbing {
		t.Error("a selector probes, but it never measures anything")
	}
}

func TestSelectingAMemberMovesNewConnectionsAndNamesThePreviousOne(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	selector := newTestSelector(t, first, second)

	previous, err := selectable(t, selector).SelectMember("second")
	if err != nil || previous != "first" {
		t.Fatalf("SelectMember = %q, %v; want first, nil", previous, err)
	}
	if used := memberUsed(t, selector, first, second); used != "second" {
		t.Errorf("used %q after the switch, want second", used)
	}
	if got := selector.(component.GroupStateReporter).CurrentMemberTag(); got != "second" {
		t.Errorf("CurrentMemberTag = %q, want second", got)
	}

	previous, err = selectable(t, selector).SelectMember("second")
	if err != nil || previous != "second" {
		t.Errorf("selecting the current member = %q, %v; want second, nil", previous, err)
	}
}

func TestAnUnknownMemberLeavesTheChoiceAlone(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	selector := newTestSelector(t, first, second)
	selectable(t, selector).SelectMember("second")

	if _, err := selectable(t, selector).SelectMember("nowhere"); !errors.Is(err, component.ErrNoSuchMember) {
		t.Errorf("SelectMember(nowhere) = %v, want ErrNoSuchMember", err)
	}
	if used := memberUsed(t, selector, first, second); used != "second" {
		t.Errorf("used %q after a refused switch, want second", used)
	}
}

func TestWhenTheChosenMemberFailsTheConnectionFailsAndNoOtherIsTried(t *testing.T) {
	broken, healthy := newMember(t, "broken", 204), newMember(t, "healthy", 204)
	failure := errors.New("refused")
	broken.dialFailure.Store(&failure)
	selector := newTestSelector(t, broken, healthy)

	_, err := selector.DialStream(context.Background(), realConnectionMetadata(t))
	if !errors.Is(err, failure) {
		t.Fatalf("DialStream = %v, want the chosen member's failure", err)
	}
	if healthy.connectionDials.Load() != 0 {
		t.Error("the selector moved to another member on its own")
	}
}

func TestASelectorCarriesOnlyTheNetworksEveryMemberDoes(t *testing.T) {
	both, tcpOnly := newMember(t, "both", 204), newMember(t, "tcp", 204)
	both.networks = []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
	selector := newTestSelector(t, both, tcpOnly)

	if networks := selector.Networks(); len(networks) != 1 || networks[0] != spec.NetworkTCP {
		t.Errorf("Networks = %v, want only TCP", networks)
	}
}

func TestADatagramComesFromTheChosenMember(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	first.networks = []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
	second.networks = []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
	selector := newTestSelector(t, first, second)
	selectable(t, selector).SelectMember("second")

	if _, err := selector.CreateDatagram(context.Background(), realConnectionMetadata(t)); err != nil {
		t.Fatal(err)
	}
	if first.connectionDials.Load() != 0 || second.connectionDials.Load() != 1 {
		t.Errorf("datagram dials: first %d, second %d; want 0 and 1", first.connectionDials.Load(), second.connectionDials.Load())
	}
}

func TestASelectorNeedsAMember(t *testing.T) {
	if _, err := group.NewSelector(group.SelectorConfig{Tag: "proxy"}); err == nil {
		t.Error("a selector with no member was built")
	}
}

func TestASelectorMayChooseAnotherGroupAndNameEveryLevelThatCarriedTheStream(t *testing.T) {
	member := newMember(t, "member", 204)
	inner, err := group.NewFallback(group.FallbackConfig{Tag: "inner", Members: []component.Outbound{member}})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := group.NewSelector(group.SelectorConfig{Tag: "outer", Members: []component.Outbound{inner}})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := outer.DialStream(context.Background(), realConnectionMetadata(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	tags := component.TagsOfOutboundsThatCarried(outer, stream)
	if len(tags) != 3 || tags[0] != "outer" || tags[1] != "inner" || tags[2] != "member" {
		t.Errorf("carrying tags = %v, want [outer inner member]", tags)
	}
}

func TestSwitchingWhileConnectionsAreBeingDialled(t *testing.T) {
	first, second := newMember(t, "first", 204), newMember(t, "second", 204)
	selector := newTestSelector(t, first, second)
	var groupOutbound sync.WaitGroup
	for range 4 {
		groupOutbound.Add(2)
		go func() {
			defer groupOutbound.Done()
			for range 50 {
				if stream, err := selector.DialStream(context.Background(), realConnectionMetadata(t)); err == nil {
					stream.Close()
				}
			}
		}()
		go func() {
			defer groupOutbound.Done()
			for index := range 50 {
				selectable(t, selector).SelectMember([]string{"first", "second"}[index%2])
			}
		}()
	}
	groupOutbound.Wait()
}
