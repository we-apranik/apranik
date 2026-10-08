package group_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/outbound/group"
	"github.com/The-Apranik/Apranik/spec"
)

type connectingMember struct{ tag string }

type unusedStream struct{ spec.Stream }

func (m connectingMember) Tag() string { return m.tag }
func (connectingMember) Networks() []spec.Network {
	return []spec.Network{spec.NetworkTCP}
}
func (connectingMember) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return unusedStream{}, nil
}
func (connectingMember) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return nil, spec.ErrUnsupportedNetwork
}
func (connectingMember) Close() error { return nil }

// What a group adds to every new connection: choosing the order to try its
// members in. The members connect instantly, so the time is the group's own.
func BenchmarkGroupDialStream(b *testing.B) {
	for _, picksFastest := range []bool{true, false} {
		for _, memberCount := range []int{3, 10} {
			name := fmt.Sprintf("fallback/%d-members", memberCount)
			if picksFastest {
				name = fmt.Sprintf("urltest/%d-members", memberCount)
			}
			b.Run(name, func(b *testing.B) {
				members := make([]component.Outbound, memberCount)
				for index := range members {
					members[index] = connectingMember{tag: fmt.Sprintf("member-%d", index)}
				}
				var groupOutbound component.ProbingOutbound
				var err error
				if picksFastest {
					groupOutbound, err = group.NewURLTest(group.URLTestConfig{Tag: "group", Members: members})
				} else {
					groupOutbound, err = group.NewFallback(group.FallbackConfig{Tag: "group", Members: members})
				}
				if err != nil {
					b.Fatal(err)
				}
				defer groupOutbound.Close()

				ctx, metadata := context.Background(), spec.Metadata{Network: spec.NetworkTCP}
				b.ReportAllocs()
				for b.Loop() {
					if _, err := groupOutbound.DialStream(ctx, metadata); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
