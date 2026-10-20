package trojan

import (
	"context"
	"fmt"
	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/shared/delayedrequest"
	"github.com/we-apranik/apranik/spec"
)

type client struct {
	passwordHash  wire.PasswordHash
	requestTiming delayedrequest.Policy
}

func (c *client) RequestStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("trojan: %w: UDP goes through RequestDatagramOverStream", spec.ErrUnsupportedNetwork)
	}
	request, err := wire.AppendRequest(nil, wire.Request{PasswordHash: c.passwordHash, Command: wire.CommandConnect, Destination: metadata.Destination})
	if err != nil {
		return nil, err
	}
	return delayedrequest.NewStream(underlyingStream, request, c.requestTiming.DelayForDestinationPort(metadata.Destination.Port())), nil
}

// Nothing goes on the wire here either: the request rides with the first
// datagram and names its destination, as Xray and sing-box clients do. A UDP
// session creates this only to send a packet, so no timer is needed.
func (c *client) RequestDatagramOverStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	return newClientStreamDatagram(underlyingStream, c.passwordHash), nil
}

func (c *client) DatagramDestinationIsWritten() spec.DatagramDestinationPlacement {
	return spec.DatagramDestinationInEveryDatagram
}
