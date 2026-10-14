package vless

import (
	"context"
	"fmt"
	"github.com/we-apranik/apranik/module/proxy/vless/wire"
	"github.com/we-apranik/apranik/shared/delayedrequest"
	"github.com/we-apranik/apranik/spec"
)

type client struct {
	uuid          wire.UUID
	requestTiming delayedrequest.Policy
	xudp          bool
}

// Nothing goes on the wire here. The request travels with the first payload,
// so a connection never opens with a header of fixed length on its own, and
// nothing is read until the application reads the answer header.
func (c *client) RequestStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("vless: %w: UDP goes through RequestDatagramOverStream", spec.ErrUnsupportedNetwork)
	}
	// Encoded now so a destination the format cannot carry fails here.
	request, err := wire.AppendRequestHeader(nil, wire.Request{UUID: c.uuid, Command: wire.CommandTCP, Destination: metadata.Destination})
	if err != nil {
		return nil, err
	}
	delayed := delayedrequest.NewStream(underlyingStream, request, c.requestTiming.DelayForDestinationPort(metadata.Destination.Port()))
	return &responseHeaderSkippingStream{Stream: delayed}, nil
}

// Nothing goes on the wire here either: the request rides with the first
// datagram and names its destination, which this stream then serves for good.
func (c *client) RequestDatagramOverStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	if c.xudp {
		return c.requestXUDP(underlyingStream, metadata.XUDPGlobalID)
	}
	return newClientDatagram(underlyingStream, c.uuid), nil
}

// DatagramDestinationIsWritten is answered here because plain VLESS names the
// one destination of a UDP stream in its request, not in each datagram, while
// XUDP names it in every datagram.
func (c *client) DatagramDestinationIsWritten() spec.DatagramDestinationPlacement {
	if c.xudp {
		return spec.DatagramDestinationInEveryDatagram
	}
	return spec.DatagramDestinationInStreamRequest
}
