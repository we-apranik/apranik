// Package handler is the component.TrafficHandler core gives every inbound. It
// asks the router where traffic goes and hands the carriers to the relay:
// once per TCP connection, and once per new destination in a UDP session,
// whose answers it caches for the life of that session. It keeps the client's
// stream to itself; the router learns about it through a traffic informant.
package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp/session"
	"github.com/The-Apranik/Apranik/core/relay"
	"github.com/The-Apranik/Apranik/spec"
)

// Handler is component.TrafficHandler. It decides nothing itself, asking the
// router about every connection and every new UDP destination.
type Handler struct {
	router                component.Router
	streamIdleTimeout     time.Duration
	udpSessionIdleTimeout time.Duration
	resolver              DomainResolver
	logger                *logging.Logger
}

// One per inbound, so each inbound's timeouts apply to its own traffic.
type Config struct {
	// Zero means relay.DefaultStreamIdleTimeout; negative means no idle bound.
	StreamIdleTimeout time.Duration

	// Zero means session.DefaultIdleTimeout.
	UDPSessionIdleTimeout time.Duration

	// What a routing resolve action asks. Nil: a resolve finds nothing.
	Resolver DomainResolver

	// Nil is allowed: New substitutes a silent logger.
	Logger *logging.Logger
}

func New(router component.Router, config Config) *Handler {
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
		resolver:              config.Resolver,
		logger:                logger,
	}
}

func (h *Handler) HandleStream(ctx context.Context, clientStream spec.Stream, metadata spec.Metadata) error {
	informant := newStreamTrafficInformant(metadata, clientStream, h.resolver, h.logger)
	decision, err := h.router.Route(ctx, informant)
	clientStream = informant.streamToRelay()
	metadata = informant.Metadata()
	if err != nil {
		clientStream.Close()
		return fmt.Errorf("handler: %s -> %s: %w", metadata.InboundTag, metadata.Destination, err)
	}
	if decision.IsBlock() {
		clientStream.Close()
		h.logger.Debug("Stream blocked", "inbound", metadata.InboundTag, "rule", decision.Rule)
		return nil
	}

	outboundStream, err := decision.Outbound.DialStream(ctx, metadata)
	if err != nil {
		clientStream.Close()
		return fmt.Errorf("handler: %s -> %s: %w", metadata.InboundTag, metadata.Destination, err)
	}

	h.logger.Debug("Relaying stream", "inbound", metadata.InboundTag, "outbound", decision.Outbound.Tag(), "rule", decision.Rule)
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

	routingCache := newUDPSessionRoutingCache(func(ctx context.Context, destination spec.Destination) (component.Decision, error) {
		return h.routeUDPDestination(ctx, metadata, destination)
	}, cachedRouteIdleTimeout)
	defer routingCache.close()

	h.logger.Debug("Relaying datagrams", "inbound", metadata.InboundTag)

	relayErr := relay.Datagrams(ctx, relay.DatagramRelayConfig{
		ClientSide:    clientDatagram,
		ClientSession: udpSession,
		OutboundForDestination: func(ctx context.Context, destination spec.Destination) (component.Outbound, error) {
			decision, err := routingCache.route(ctx, destination)
			if err != nil {
				return nil, err
			}
			if decision.IsBlock() {
				return nil, relay.ErrDestinationBlocked
			}
			return decision.Outbound, nil
		},
		Logger: h.logger,
	})
	switch {
	case errors.Is(relayErr, session.ErrIdle), errors.Is(relayErr, session.ErrClosed):
		return nil
	default:
		return relayErr
	}
}

func (h *Handler) routeUDPDestination(ctx context.Context, sessionMetadata spec.Metadata, destination spec.Destination) (component.Decision, error) {
	packetMetadata := sessionMetadata
	packetMetadata.Network = spec.NetworkUDP
	packetMetadata.Destination = destination

	decision, err := h.router.Route(ctx, &udpSessionTrafficInformant{metadata: packetMetadata, resolver: h.resolver, logger: h.logger})
	if err != nil {
		return component.Decision{}, fmt.Errorf("handler: routing %s: %w", destination, err)
	}
	h.logger.Debug("Routed UDP destination", "inbound", sessionMetadata.InboundTag, "rule", decision.Rule, "blocked", decision.IsBlock())
	return decision, nil
}
