package apicoreprovider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/outbound"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/shared/httpstatusranges"
	"github.com/we-apranik/apranik/spec"
)

// OutboundTags is a read-only slice made once: the outbounds do not change
// while core runs.
func (p *Provider) OutboundTags() []string { return p.outboundTags }

func (p *Provider) OutboundState(outboundTag string) (spec.APIOutboundState, error) {
	built, found := p.outboundsByTag[outboundTag]
	if !found {
		return spec.APIOutboundState{}, spec.ErrAPINoSuchOutbound
	}
	described := p.blueprintByTag[outboundTag]

	state := spec.APIOutboundState{
		Tag:        outboundTag,
		CarriesUDP: slices.Contains(built.Networks(), spec.NetworkUDP),
	}
	switch {
	case described.Group != nil:
		state.Kind = groupKind(described.Group.Type)
		if reporter, reports := built.(component.GroupStateReporter); reports {
			state.Group = &spec.APIGroupState{MemberTags: reporter.MemberTags(), CurrentMember: reporter.CurrentMemberTag()}
		}
	case described.Freedom != nil:
		state.Kind = spec.APIOutboundKindFreedom
	default:
		state.Kind = spec.APIOutboundKindProxy
		if described.Proxy != nil {
			state.ProtocolName = described.Proxy.Info().Name
		}
	}
	return state, nil
}

func groupKind(groupType blueprint.GroupType) spec.APIOutboundKind {
	switch groupType {
	case blueprint.GroupTypeURLTest:
		return spec.APIOutboundKindURLTest
	case blueprint.GroupTypeFallback:
		return spec.APIOutboundKindFallback
	default:
		return spec.APIOutboundKindSelector
	}
}

// DefaultOutboundTag is where a request goes when no rule decides: the default
// action's outbound, or the first outbound when there is no routing section.
func (p *Provider) DefaultOutboundTag() string {
	routing := p.blueprint.Routing
	switch {
	case routing == nil && len(p.outboundTags) > 0:
		return p.outboundTags[0]
	case routing == nil, routing.DefaultAction.Route == nil:
		return ""
	default:
		return routing.DefaultAction.Route.Outbound
	}
}

// MeasureDelay checks the request and has no defaults: the caller passes every value.
func (p *Provider) MeasureDelay(ctx context.Context, outboundTag, rawURL string, timeout time.Duration, expectedStatus string) (time.Duration, error) {
	target, found := p.outboundsByTag[outboundTag]
	if !found {
		return 0, spec.ErrAPINoSuchOutbound
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%w: a delay test needs a timeout", spec.ErrAPIInvalidDelayTest)
	}
	expected, err := httpstatusranges.Parse(expectedStatus)
	if err != nil {
		return 0, fmt.Errorf("%w: expected status: %w", spec.ErrAPIInvalidDelayTest, err)
	}
	tester, err := outbound.NewDelayTester(rawURL, expected)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", spec.ErrAPIInvalidDelayTest, err)
	}
	return tester.MeasureDelay(ctx, target, timeout)
}

// SelectMember switches the selector, then ends the live conversations that went
// through the member it replaced, so a switch takes hold for connections that
// are already open, and for UDP sessions, which choose their member once. A
// connection dialled through the old member that is not listed yet when the
// connections are closed keeps its member: a known limit, not guarded.
func (p *Provider) SelectMember(selectorTag, memberTag string) error {
	built, found := p.outboundsByTag[selectorTag]
	if !found {
		return spec.ErrAPINoSuchOutbound
	}
	selectable, isSelectable := built.(component.SelectableOutbound)
	if !isSelectable {
		return fmt.Errorf("%w: %q is a %s", spec.ErrAPIOutboundNotSelectable, selectorTag, p.describeKind(selectorTag))
	}

	previousMember, err := selectable.SelectMember(memberTag)
	switch {
	case errors.Is(err, component.ErrNoSuchMember):
		return fmt.Errorf("%w: %w", spec.ErrAPINoSuchMember, err)
	case err != nil:
		return err
	}

	if !p.stats.IsLiveTrackingEnabled() {
		p.logger.Info("Selector switched; live conversations are not tracked, so open connections stay", "selector", selectorTag, "from", previousMember, "to", memberTag)
		return nil
	}
	return p.stats.CloseLiveMatchingConversations(func(info stats.ConversationInfo) bool {
		return wentThrough(info.OutboundTags, selectorTag, previousMember)
	})
}

func (p *Provider) describeKind(outboundTag string) string {
	if state, err := p.OutboundState(outboundTag); err == nil {
		return string(state.Kind) + " outbound"
	}
	return "outbound"
}

// A connection went through a member of a selector when the selector's tag is
// followed by the member's in the list of outbounds that carried it. A
// connection to the same member by another route, or through the new member,
// does not match.
func wentThrough(carryingTags []string, selectorTag, memberTag string) bool {
	for index := 0; index+1 < len(carryingTags); index++ {
		if carryingTags[index] == selectorTag && carryingTags[index+1] == memberTag {
			return true
		}
	}
	return false
}
