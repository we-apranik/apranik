// Package handler is the component.TrafficHandler core gives every inbound.
package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/udp"
	"github.com/we-apranik/apranik/core/net/udp/resumable"
	"github.com/we-apranik/apranik/core/net/udp/session"
	"github.com/we-apranik/apranik/core/relay"
	"github.com/we-apranik/apranik/core/route/informant"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

type Handler struct {
	router                              component.Router
	streamIdleTimeout                   time.Duration
	udpSessionIdleTimeout               time.Duration
	udpRoutingCacheEntryIdleTimeout     time.Duration
	udpRoutingCacheMaxEntriesPerSession int

	liveTrackingUDPEntryIdleTimeout     time.Duration
	liveTrackingUDPMaxEntriesPerSession int

	udpMaxDatagramSize int
	udpReplyBuffers    *udp.BufferPool

	resumableUDPSessions *resumable.Registry

	udpSweeper *periodicSweeper

	resolver informant.DomainResolver
	stats    *stats.Registry

	inboundProtocolName string
	logger              *logging.Logger
}

type Config struct {
	StreamIdleTimeout time.Duration

	UDPSessionIdleTimeout time.Duration

	UDPCountFlushInterval time.Duration

	UDPRoutingCacheEntryIdleTimeout time.Duration

	UDPRoutingCacheMaxEntriesPerSession int

	UDPMaxDatagramSize int

	LiveTrackingUDPEntryIdleTimeout time.Duration

	LiveTrackingUDPMaxEntriesPerSession int

	// A UDP session whose stream is lost is held, and given to the next stream of the same user with the
	// same XUDP Global ID (spec.Metadata.XUDPGlobalID).
	ResumeUDPSessions        bool
	UDPSessionResumeHoldTime time.Duration

	Resolver informant.DomainResolver

	InboundProtocolName string

	Stats *stats.Registry

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
	udpRoutingCacheEntryIdleTimeout := config.UDPRoutingCacheEntryIdleTimeout
	if udpRoutingCacheEntryIdleTimeout <= 0 {
		udpRoutingCacheEntryIdleTimeout = udp.DefaultRoutingCacheEntryIdleTimeout
	}
	udpRoutingCacheMaxEntriesPerSession := config.UDPRoutingCacheMaxEntriesPerSession
	if udpRoutingCacheMaxEntriesPerSession <= 0 {
		udpRoutingCacheMaxEntriesPerSession = udp.DefaultRoutingCacheMaxEntriesPerSession
	}
	liveTrackingUDPEntryIdleTimeout := config.LiveTrackingUDPEntryIdleTimeout
	if liveTrackingUDPEntryIdleTimeout <= 0 {
		liveTrackingUDPEntryIdleTimeout = udp.DefaultLiveTrackingUDPEntryIdleTimeout
	}
	liveTrackingUDPMaxEntriesPerSession := config.LiveTrackingUDPMaxEntriesPerSession
	if liveTrackingUDPMaxEntriesPerSession <= 0 {
		liveTrackingUDPMaxEntriesPerSession = udp.DefaultLiveTrackingUDPMaxEntriesPerSession
	}
	udpMaxDatagramSize := config.UDPMaxDatagramSize
	if udpMaxDatagramSize <= 0 {
		udpMaxDatagramSize = udp.DefaultMaxDatagramSize
	}
	countFlushInterval := config.UDPCountFlushInterval
	if countFlushInterval <= 0 {
		countFlushInterval = defaultCountFlushInterval
	}
	sweepInterval := min(sweepIntervalFor(min(udpRoutingCacheEntryIdleTimeout, liveTrackingUDPEntryIdleTimeout)), countFlushInterval)
	var resumableUDPSessions *resumable.Registry
	if config.ResumeUDPSessions {
		resumableUDPSessions = resumable.NewRegistry(config.UDPSessionResumeHoldTime)
	}
	return &Handler{
		router:                              router,
		streamIdleTimeout:                   streamIdleTimeout,
		udpSessionIdleTimeout:               config.UDPSessionIdleTimeout,
		udpRoutingCacheEntryIdleTimeout:     udpRoutingCacheEntryIdleTimeout,
		udpRoutingCacheMaxEntriesPerSession: udpRoutingCacheMaxEntriesPerSession,

		liveTrackingUDPEntryIdleTimeout:     liveTrackingUDPEntryIdleTimeout,
		liveTrackingUDPMaxEntriesPerSession: liveTrackingUDPMaxEntriesPerSession,
		udpMaxDatagramSize:                  udpMaxDatagramSize,
		udpReplyBuffers:                     udp.NewBufferPool(udpMaxDatagramSize),
		resumableUDPSessions:                resumableUDPSessions,
		udpSweeper:                          newPeriodicSweeper(sweepInterval),
		resolver:                            config.Resolver,
		stats:                               config.Stats,
		inboundProtocolName:                 config.InboundProtocolName,
		logger:                              logger,
	}
}

func (h *Handler) HandleStream(ctx context.Context, clientStream spec.Stream, metadata spec.Metadata) error {
	trafficInformant := informant.NewStream(metadata, clientStream, h.resolver, h.logger)
	decision, err := h.router.Route(ctx, trafficInformant)
	clientStream = trafficInformant.StreamToRelay()
	metadata = trafficInformant.Metadata()
	if err != nil {
		clientStream.Close()
		return fmt.Errorf("handler: %s -> %s: %w", metadata.InboundTag, metadata.Destination, err)
	}
	h.stats.RuleHit(decision.Rule)
	if decision.IsBlock() {
		clientStream.Close()
		h.stats.Inbound(metadata.InboundTag).ConnectionRefusedFor(stats.RefusalBlockedByRule)
		h.logger.Debug("Stream blocked", "inbound", metadata.InboundTag, "rule", decision.Rule)
		return nil
	}

	timesDials := h.stats.TimesDials()
	var dialStarted time.Time
	if timesDials {
		dialStarted = time.Now()
	}
	outboundStream, err := decision.Outbound.DialStream(ctx, metadata)
	if err == nil && timesDials {
		h.stats.Outbound(decision.Outbound.Tag()).DialSucceededIn(time.Since(dialStarted))
	}
	if err != nil {
		clientStream.Close()
		h.stats.Outbound(decision.Outbound.Tag()).DialFailedFor(classifyDialError(err))
		return fmt.Errorf("handler: %s -> %s: %w", metadata.InboundTag, metadata.Destination, err)
	}

	var carryingOutboundTags []string
	if h.stats != nil {
		carryingOutboundTags = component.TagsOfOutboundsThatCarried(decision.Outbound, outboundStream)
	}
	counters := h.countersCarrying(metadata, carryingOutboundTags)
	relayContext, closeRequested := ctx, context.CancelFunc(nil)
	if h.stats.IsLiveTrackingEnabled() {
		relayContext, closeRequested = context.WithCancel(ctx)
		defer closeRequested()
	}
	connection := h.stats.TrackConversation(stats.ConversationInfo{
		Network:         metadata.Network,
		InboundTag:      metadata.InboundTag,
		InboundProtocol: h.inboundProtocolName,
		User:            metadata.User,
		Source:          metadata.Transport.SourceEndpoint,
		Destination:     metadata.Destination,
		SniffedDomain:   sniffedDomainOf(metadata),
		OutboundTags:    carryingOutboundTags,
		Rule:            decision.Rule,
	}, closeRequested)
	defer connection.End()
	for _, counted := range counters {
		counted.ConnectionStarted()
	}
	defer func() {
		for _, counted := range counters {
			counted.ConnectionEnded()
		}
	}()

	h.logger.Debug("Relaying stream", "inbound", metadata.InboundTag, "outbound", decision.Outbound.Tag(), "rule", decision.Rule)
	relayErr := relay.Stream(relayContext, clientStream, outboundStream, h.streamIdleTimeout, counters, connection.Bytes())
	switch {
	case errors.Is(relayErr, relay.ErrStreamIdle):
		return nil
	case errors.Is(relayErr, context.Canceled) && ctx.Err() == nil:
		h.logger.Info("Connection closed by request", "inbound", metadata.InboundTag, "outbound", decision.Outbound.Tag(), "rule", decision.Rule)
		return nil
	}
	return relayErr
}

func (h *Handler) HandleDatagram(ctx context.Context, clientDatagram spec.Datagram, metadata spec.Metadata) error {
	if h.resumableUDPSessions != nil && metadata.XUDPGlobalID != ([8]byte{}) {
		key := resumable.Key{User: metadata.User, GlobalID: metadata.XUDPGlobalID}
		standIn, resumed, streamEnded := h.resumableUDPSessions.Connect(key, clientDatagram)
		if resumed {
			h.logger.Debug("UDP session resumed", "inbound", metadata.InboundTag, "user", metadata.User)
			select {
			case <-streamEnded:
			case <-ctx.Done():
			}
			return nil
		}
		clientDatagram = standIn
	}

	parentContext := ctx
	closeSession := context.CancelFunc(nil)
	if h.stats.IsLiveTrackingEnabled() {
		ctx, closeSession = context.WithCancel(ctx)
		defer closeSession()
	}
	udpSession := session.New(ctx, session.Config{
		Metadata:             metadata,
		IdleTimeout:          h.udpSessionIdleTimeout,
		Stats:                h.stats,
		Buffers:              h.udpReplyBuffers,
		DialFailureReasonFor: classifyDialError,
		Logger:               h.logger,
	})

	countFlusher := sessionCountFlusher{session: udpSession}
	h.udpSweeper.add(countFlusher)
	defer h.udpSweeper.remove(countFlusher)

	routingCache := newUDPSessionRoutingCache(func(ctx context.Context, destination spec.Destination) (component.Decision, error) {
		return h.routeUDPDestination(ctx, metadata, destination)
	}, h.udpRoutingCacheEntryIdleTimeout, h.udpRoutingCacheMaxEntriesPerSession)
	h.udpSweeper.add(routingCache)
	defer func() {
		h.udpSweeper.remove(routingCache)
		routingCache.close()
	}()

	var replyReceived func(from spec.Destination, size int)
	if h.stats.IsLiveTrackingEnabled() {
		live := newUDPSessionLiveEntries(h.stats, stats.ConversationInfo{
			Network:         spec.NetworkUDP,
			InboundTag:      metadata.InboundTag,
			InboundProtocol: h.inboundProtocolName,
			User:            metadata.User,
			Source:          metadata.Transport.SourceEndpoint,
		}, func(routed component.Outbound) []string {
			if tags := udpSession.CarryingOutboundTags(routed); tags != nil {
				return tags
			}
			return []string{routed.Tag()}
		}, closeSession, h.liveTrackingUDPEntryIdleTimeout, h.liveTrackingUDPMaxEntriesPerSession)
		h.udpSweeper.add(live)
		defer func() {
			h.udpSweeper.remove(live)
			live.close()
		}()

		routingCache.newUplinkAccepted = func(destination spec.Destination, decision component.Decision) func(size int) {
			return func(size int) { live.uplinkAccepted(destination, decision, size) }
		}
		replyReceived = func(from spec.Destination, size int) {
			if live.downlinkReceived(from, size) {
				return
			}
			if decision, awaitingFirstReport := routingCache.decisionAwaitingFirstUplinkReport(from); awaitingFirstReport {
				live.downlinkReceivedFromRoutedDestination(from, decision, size)
			}
		}
	}

	h.logger.Debug("Relaying datagrams", "inbound", metadata.InboundTag)

	relayErr := relay.Datagrams(ctx, relay.DatagramRelayConfig{
		ClientSide:    clientDatagram,
		ClientSession: udpSession,
		OutboundForDestination: func(ctx context.Context, destination spec.Destination) (relay.RoutedDestination, error) {
			cached, err := routingCache.route(ctx, destination)
			if err != nil {
				return relay.RoutedDestination{}, err
			}
			if cached.decision.IsBlock() {
				return relay.RoutedDestination{}, relay.ErrDestinationBlocked
			}
			routed := relay.RoutedDestination{Outbound: cached.decision.Outbound}
			routed.UplinkAccepted = cached.uplinkAccepted
			return routed, nil
		},
		ReplyReceived:   replyReceived,
		MaxDatagramSize: h.udpMaxDatagramSize,
		Stats:           h.stats,
		InboundTag:      metadata.InboundTag,
		Logger:          h.logger,
	})
	switch {
	case errors.Is(relayErr, session.ErrIdle), errors.Is(relayErr, session.ErrClosed):
		return nil
	case errors.Is(relayErr, context.Canceled) && parentContext.Err() == nil:
		h.logger.Info("UDP session closed by request", "inbound", metadata.InboundTag)
		return nil
	default:
		return relayErr
	}
}

func (h *Handler) routeUDPDestination(ctx context.Context, sessionMetadata spec.Metadata, destination spec.Destination) (component.Decision, error) {
	packetMetadata := sessionMetadata
	packetMetadata.Network = spec.NetworkUDP
	packetMetadata.Destination = destination

	decision, err := h.router.Route(ctx, informant.NewUDPSession(packetMetadata, h.resolver, h.logger))
	if err != nil {
		return component.Decision{}, fmt.Errorf("handler: routing %s: %w", destination, err)
	}
	h.stats.RuleHit(decision.Rule)
	h.logger.Debug("Routed UDP destination", "inbound", sessionMetadata.InboundTag, "rule", decision.Rule, "blocked", decision.IsBlock())
	return decision, nil
}

// Worked out once per connection, never per read.
func (h *Handler) countersCarrying(metadata spec.Metadata, carryingOutboundTags []string) []*stats.Counters {
	if h.stats == nil {
		return nil
	}
	counters := make([]*stats.Counters, 0, 2+len(carryingOutboundTags))
	if inbound := h.stats.Inbound(metadata.InboundTag).Base(); inbound != nil {
		counters = append(counters, inbound)
	}
	if user := h.stats.User(metadata.User); user != nil {
		counters = append(counters, user)
	}
	for _, tag := range carryingOutboundTags {
		if outbound := h.stats.Outbound(tag).Base(); outbound != nil {
			counters = append(counters, outbound)
		}
	}
	return counters
}

func sniffedDomainOf(metadata spec.Metadata) string {
	if metadata.Sniffed == nil {
		return ""
	}
	return metadata.Sniffed.Domain
}
