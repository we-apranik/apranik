package apicoreprovider

import (
	"errors"
	"fmt"

	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

func (p *Provider) LiveConversations() ([]spec.APILiveConversation, error) {
	if p.stats == nil {
		return nil, spec.ErrAPIStatisticsDisabled
	}
	if !p.stats.IsLiveTrackingEnabled() {
		return nil, spec.ErrAPILiveConversationsDisabled
	}
	snapshots := p.stats.LiveConversations()
	conversations := make([]spec.APILiveConversation, 0, len(snapshots))
	for _, snapshot := range snapshots {
		conversations = append(conversations, spec.APILiveConversation{
			ID:              snapshot.ID,
			Network:         snapshot.Info.Network,
			InboundTag:      snapshot.Info.InboundTag,
			InboundProtocol: snapshot.Info.InboundProtocol,
			SniffedDomain:   snapshot.Info.SniffedDomain,
			User:            snapshot.Info.User,
			Source:          snapshot.Info.Source,
			Destination:     snapshot.Info.Destination,
			StartedAt:       snapshot.StartedAt,
			OutboundTags:    snapshot.Info.OutboundTags,
			RuleLocation:    snapshot.Info.Rule,
			UplinkBytes:     snapshot.UplinkBytes,
			DownlinkBytes:   snapshot.DownlinkBytes,
		})
	}
	return conversations, nil
}

func (p *Provider) CloseLiveConversation(id string) error {
	if p.stats == nil {
		return spec.ErrAPIStatisticsDisabled
	}
	p.logger.Info("Live conversation close requested", "conversation", id)
	return translateClosingError(p.stats.CloseLiveConversation(id))
}

func (p *Provider) CloseAllLiveConversations() error {
	if p.stats == nil {
		return spec.ErrAPIStatisticsDisabled
	}
	p.logger.Info("Closing every live conversation was requested")
	return translateClosingError(p.stats.CloseLiveMatchingConversations(func(stats.ConversationInfo) bool { return true }))
}

func translateClosingError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, stats.ErrLiveConversationNotFound):
		return fmt.Errorf("%w: %w", spec.ErrAPINoSuchLiveConversation, err)
	case errors.Is(err, stats.ErrLiveConversationsNotTracked):
		return fmt.Errorf("%w: %w", spec.ErrAPILiveConversationsDisabled, err)
	default:
		return err
	}
}
