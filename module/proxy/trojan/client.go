package trojan

import (
	"context"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/trojan/wire"
	"github.com/The-Apranik/Apranik/shared/delayedrequest"
	"github.com/The-Apranik/Apranik/spec"
)

type client struct {
	passwordHash         wire.PasswordHash
	maxFirstPayloadDelay time.Duration
}

// Nothing goes on the wire here. The request travels with the first payload,
// so a connection never opens with a header of fixed length on its own. There
// is no reply either, so a wrong password surfaces as the server closing the
// stream.
func (c *client) RequestStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("trojan: %w: UDP goes through RequestDatagramOverStream", spec.ErrUnsupportedNetwork)
	}
	// Encoded now so a destination the format cannot carry fails here.
	request, err := wire.AppendRequest(nil, wire.Request{PasswordHash: c.passwordHash, Command: wire.CommandConnect, Destination: metadata.Destination})
	if err != nil {
		return nil, err
	}
	return delayedrequest.NewStream(underlyingStream, request, c.maxFirstPayloadDelay), nil
}

// Nothing goes on the wire here either: the request rides with the first
// datagram and names its destination, as Xray and sing-box clients do. A UDP
// session creates this only to send a packet, so no timer is needed.
func (c *client) RequestDatagramOverStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	return newClientStreamDatagram(underlyingStream, c.passwordHash), nil
}

// Trojan names the destination in every UDP frame, so one stream carries all of
// a UDP session's destinations.
func (c *client) DatagramDestinationIsWritten() spec.DatagramDestinationPlacement {
	return spec.DatagramDestinationInEveryDatagram
}
