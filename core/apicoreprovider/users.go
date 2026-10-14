package apicoreprovider

import (
	"encoding/json"
	"fmt"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

// An inbound that does not exist and one whose protocol cannot change its
// users are the same to the API: there is no inbound here to manage users on.
// The message says which.
func (p *Provider) userManagerOf(inboundTag string) (spec.UserManager, error) {
	built, found := p.inboundsByTag[inboundTag]
	if !found {
		return nil, fmt.Errorf("%w: there is no inbound %q", spec.ErrAPINoSuchInbound, inboundTag)
	}
	if managing, isManaging := built.(component.UserManagingInbound); isManaging {
		if manager := managing.UserManager(); manager != nil {
			return manager, nil
		}
	}
	return nil, fmt.Errorf("%w: inbound %q does not manage users", spec.ErrAPINoSuchInbound, inboundTag)
}

func (p *Provider) ListInboundUsers(inboundTag string) ([]string, error) {
	manager, err := p.userManagerOf(inboundTag)
	if err != nil {
		return nil, err
	}
	return manager.ListUsers(), nil
}

// The protocol's own errors (spec.ErrInvalidUser, spec.ErrUserExists) are
// returned as they are: they are already in the contract.
func (p *Provider) AddInboundUser(inboundTag string, user json.RawMessage) (string, error) {
	manager, err := p.userManagerOf(inboundTag)
	if err != nil {
		return "", err
	}
	name, err := manager.AddUser(user)
	if err != nil {
		return "", err
	}
	p.logger.Info("User added", "inbound", inboundTag, "user", name)
	return name, nil
}

// Only the removed user's conversations on this inbound end; the same name on
// another inbound is another inbound's user and keeps working.
func (p *Provider) RemoveInboundUser(inboundTag, name string) error {
	manager, err := p.userManagerOf(inboundTag)
	if err != nil {
		return err
	}
	if err := manager.RemoveUser(name); err != nil {
		return err
	}
	p.logger.Info("User removed", "inbound", inboundTag, "user", name)

	if !p.stats.IsLiveTrackingEnabled() {
		p.logger.Info("Live conversations are not tracked, so the removed user's open connections stay", "inbound", inboundTag, "user", name)
		return nil
	}
	return p.stats.CloseLiveMatchingConversations(func(info stats.ConversationInfo) bool {
		return info.InboundTag == inboundTag && info.User == name
	})
}
