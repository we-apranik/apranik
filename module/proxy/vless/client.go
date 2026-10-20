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

func (c *client) RequestStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("vless: %w: UDP goes through RequestDatagramOverStream", spec.ErrUnsupportedNetwork)
	}
	request, err := wire.AppendRequestHeader(nil, wire.Request{UUID: c.uuid, Command: wire.CommandTCP, Destination: metadata.Destination})
	if err != nil {
		return nil, err
	}
	delayed := delayedrequest.NewStream(underlyingStream, request, c.requestTiming.DelayForDestinationPort(metadata.Destination.Port()))
	return &responseHeaderSkippingStream{Stream: delayed}, nil
}

func (c *client) RequestDatagramOverStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	if c.xudp {
		return c.requestXUDP(underlyingStream, metadata.XUDPGlobalID)
	}
	return newClientDatagram(underlyingStream, c.uuid), nil
}

func (c *client) DatagramDestinationIsWritten() spec.DatagramDestinationPlacement {
	if c.xudp {
		return spec.DatagramDestinationInEveryDatagram
	}
	return spec.DatagramDestinationInStreamRequest
}
