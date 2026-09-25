package outbound

import (
	"context"
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/spec"
)

var ErrBlocked = errors.New("outbound: blocked")

// NewBlock refuses every request, so routing can send traffic to it to drop it.
func NewBlock(tag string) spec.Outbound { return blockOutbound{tag: tag} }

type blockOutbound struct{ tag string }

func (o blockOutbound) Tag() string { return o.tag }

// Both, so routing never skips it as unable to carry a network.
func (blockOutbound) Networks() []spec.Network {
	return []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
}

func (o blockOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return nil, fmt.Errorf("outbound %q: %w", o.tag, ErrBlocked)
}

func (o blockOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return nil, fmt.Errorf("outbound %q: %w", o.tag, ErrBlocked)
}

func (blockOutbound) Close() error { return nil }
