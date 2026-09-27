// Package handler is the spec.TrafficHandler core gives every inbound. It
// asks the router for an outbound and hands the carriers to the relay:
// once per TCP connection, and once per new destination in a UDP session,
// whose answers it caches for the life of that session.
package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp/session"
	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/core/relay"
	"github.com/The-Apranik/Apranik/spec"
)

// Handler is spec.TrafficHandler: the one thing a module calls back into. It
// decides nothing itself, asking the router for every outbound.
type Handler struct {
	router                spec.Router
	streamIdleTimeout     time.Duration
	udpSessionIdleTimeout time.Duration
	logger                *logging.Logger
}

// One per inbound, so each inbound's timeouts apply to its own traffic.
type Config struct {
	// Zero means relay.DefaultStreamIdleTimeout; negative means no idle bound.
	StreamIdleTimeout time.Duration

	// Zero means session.DefaultIdleTimeout.
	UDPSessionIdleTimeout time.Duration

	// Nil is allowed: New substitutes a silent logger.
	Logger *logging.Logger
}

func New(router spec.Router, config Config) *Handler {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}
	streamIdleTimeout := config.StreamIdleTimeout
	if streamIdleTimeout == 0 {
		streamIdleTimeout = relay.DefaultStreamIdleTimeout
	}
	return &Handler{
		router:                router,
		streamIdleTimeout:     streamIdleTimeout,
		udpSessionIdleTimeout: config.UDPSessionIdleTimeout,
		logger:                logger,
	}
}

func (h *Handler) HandleStream(ctx context.Context, clientStream spec.Stream, metadata spec.Metadata) error {
	selected, err := h.router.Route(ctx, metadata)
	if err != nil {
		clientStream.Close()
		return fmt.Errorf("handler: %s -> %s: %w", metadata.InboundTag, metadata.Destination, err)
	}

	outboundStream, err := selected.DialStream(ctx, metadata)
	if errors.Is(err, outbound.ErrBlocked) {
		clientStream.Close()
		h.logger.Debug("Stream blocked", "inbound", metadata.InboundTag, "outbound", selected.Tag())
		return nil
	}
	if err != nil {
		clientStream.Close()
		return fmt.Errorf("handler: %s -> %s: %w", metadata.InboundTag, metadata.Destination, err)
	}

	h.logger.Debug("Relaying stream", "inbound", metadata.InboundTag, "outbound", selected.Tag())
	relayErr := relay.Stream(ctx, clientStream, outboundStream, h.streamIdleTimeout)
	if errors.Is(relayErr, relay.ErrStreamIdle) {
		return nil
	}
	return relayErr
}

// The handler owns the UDP session and closes it when the relay ends. The
// relay asks for every packet; a per-session cache answers repeats. A session
// that ends quietly, by Close or by going idle, is not an error.
func (h *Handler) HandleDatagram(ctx context.Context, clientDatagram spec.Datagram, metadata spec.Metadata) error {
	udpSession := session.New(ctx, session.Config{Metadata: metadata, IdleTimeout: h.udpSessionIdleTimeout, Logger: h.logger})

	routingCache := newUDPSessionRoutingCache(func(ctx context.Context, destination spec.Destination) (spec.Outbound, error) {
		return h.routeUDPDestination(ctx, metadata, destination)
	}, cachedRouteIdleTimeout)
	defer routingCache.close()

	h.logger.Debug("Relaying datagrams", "inbound", metadata.InboundTag)

	relayErr := relay.Datagrams(ctx, relay.DatagramRelayConfig{
		ClientSide:             clientDatagram,
		ClientSession:          udpSession,
		OutboundForDestination: routingCache.route,
		Logger:                 h.logger,
	})
	switch {
	case errors.Is(relayErr, session.ErrIdle), errors.Is(relayErr, session.ErrClosed):
		return nil
	default:
		return relayErr
	}
}

func (h *Handler) routeUDPDestination(ctx context.Context, sessionMetadata spec.Metadata, destination spec.Destination) (spec.Outbound, error) {
	packetMetadata := sessionMetadata
	packetMetadata.Network = spec.NetworkUDP
	packetMetadata.Destination = destination

	selected, err := h.router.Route(ctx, packetMetadata)
	if err != nil {
		return nil, fmt.Errorf("handler: routing %s: %w", destination, err)
	}
	if selected == nil {
		return nil, fmt.Errorf("handler: routing %s: no outbound", destination)
	}
	return selected, nil
}
