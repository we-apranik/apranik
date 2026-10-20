package group

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/spec"
)

// memberSet is what every outbound group shares: its tag, its members and the
// walk through them in an order the group chooses. Members are built and owned
// by the caller: closing a group never closes them.
type memberSet struct {
	tag     string
	members []component.Outbound
	logger  *logging.Logger
}

func newMemberSet(tag string, members []component.Outbound, logger *logging.Logger) (memberSet, error) {
	if len(members) == 0 {
		return memberSet{}, fmt.Errorf("outbound %q: a group needs at least one member", tag)
	}
	if logger == nil {
		logger = logging.Discard()
	}
	return memberSet{tag: tag, members: members, logger: logger.With("outbound", tag)}, nil
}

func (m *memberSet) Tag() string { return m.tag }

func (m *memberSet) MemberTags() []string {
	tags := make([]string, len(m.members))
	for index, member := range m.members {
		tags[index] = member.Tag()
	}
	return tags
}

// Only what every member carries, so a group never sends a connection through
// a member that treats it differently from the others: UDP through a group of
// a proxy and freedom would otherwise leave directly whenever the proxy was down.
func (m *memberSet) Networks() []spec.Network {
	networks := slices.Clone(m.members[0].Networks())
	for _, member := range m.members[1:] {
		memberNetworks := member.Networks()
		networks = slices.DeleteFunc(networks, func(network spec.Network) bool {
			return !slices.Contains(memberNetworks, network)
		})
	}
	return networks
}

func (m *memberSet) dialStreamInOrder(ctx context.Context, metadata spec.Metadata, order []int) (spec.Stream, error) {
	var failures []error
	for _, index := range order {
		member := m.members[index]
		stream, err := member.DialStream(ctx, metadata)
		if err == nil {
			m.logger.Debug("Group member chosen", "member", member.Tag())
			return carriedStream{Stream: stream, carryingOutboundTags: component.TagsOfOutboundsThatCarried(member, stream)}, nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, m.everyMemberFailed(failures)
}

// A UDP session asks once and keeps the datagram, so the member is chosen once
// per session and a later probe never moves a session mid-flight.
func (m *memberSet) createDatagramInOrder(ctx context.Context, metadata spec.Metadata, order []int) (spec.Datagram, error) {
	var failures []error
	for _, index := range order {
		member := m.members[index]
		datagram, err := member.CreateDatagram(ctx, metadata)
		if err == nil {
			m.logger.Debug("Group member chosen", "member", member.Tag())
			return carriedDatagram{Datagram: datagram, carryingOutboundTags: component.TagsOfOutboundsThatCarried(member, datagram)}, nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, m.everyMemberFailed(failures)
}

func (m *memberSet) everyMemberFailed(failures []error) error {
	return fmt.Errorf("outbound %q: every member failed: %w", m.tag, errors.Join(failures...))
}

type carriedStream struct {
	spec.Stream
	carryingOutboundTags []string
}

func (s carriedStream) CarryingOutboundTags() []string { return s.carryingOutboundTags }

type carriedDatagram struct {
	spec.Datagram
	carryingOutboundTags []string
}

func (d carriedDatagram) CarryingOutboundTags() []string { return d.carryingOutboundTags }
