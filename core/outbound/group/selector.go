package group

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

type SelectorConfig struct {
	Tag string

	// Built and owned by the caller: closing the selector never closes them.
	// The first is where the selector starts.
	Members []component.Outbound

	Logger *logging.Logger
}

// selector is an outbound group a person drives: it uses one member, the one
// last chosen through SelectMember, and starts on the first. It never measures
// anything and never moves by itself: when the chosen member fails the
// connection fails, because the choice is the person's. Automatic behaviour
// comes from choosing a fallback or urltest group as the member.
type selector struct {
	memberSet
	currentIndex atomic.Int32
}

func NewSelector(config SelectorConfig) (component.Outbound, error) {
	members, err := newMemberSet(config.Tag, config.Members, config.Logger)
	if err != nil {
		return nil, err
	}
	return &selector{memberSet: members}, nil
}

func (s *selector) currentOrder() []int { return []int{int(s.currentIndex.Load())} }

func (s *selector) DialStream(ctx context.Context, metadata spec.Metadata) (spec.Stream, error) {
	return s.dialStreamInOrder(ctx, metadata, s.currentOrder())
}

func (s *selector) CreateDatagram(ctx context.Context, metadata spec.Metadata) (spec.Datagram, error) {
	return s.createDatagramInOrder(ctx, metadata, s.currentOrder())
}

func (s *selector) Close() error { return nil }

func (s *selector) CurrentMemberTag() string {
	return s.members[s.currentIndex.Load()].Tag()
}

func (s *selector) SelectMember(memberTag string) (string, error) {
	for index, member := range s.members {
		if member.Tag() != memberTag {
			continue
		}
		previous := s.members[s.currentIndex.Swap(int32(index))]
		s.logger.Info("Selector switched member", "from", previous.Tag(), "to", memberTag)
		return previous.Tag(), nil
	}
	return "", fmt.Errorf("outbound %q: %w: %q", s.tag, component.ErrNoSuchMember, memberTag)
}
