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

func wentThrough(carryingTags []string, selectorTag, memberTag string) bool {
	for index := 0; index+1 < len(carryingTags); index++ {
		if carryingTags[index] == selectorTag && carryingTags[index+1] == memberTag {
			return true
		}
	}
	return false
}
