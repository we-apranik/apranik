package relay

import (
	"context"
	"errors"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/udp"
	"github.com/we-apranik/apranik/core/net/udp/session"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

var ErrDestinationBlocked = errors.New("relay: destination blocked by routing")

type RoutedDestination struct {
	Outbound component.Outbound

	UplinkAccepted func(size int)
}

type DatagramRelayConfig struct {
	ClientSide    spec.Datagram
	ClientSession *session.Session

	OutboundForDestination func(ctx context.Context, destination spec.Destination) (RoutedDestination, error)

	ReplyReceived func(from spec.Destination, size int)

	MaxDatagramSize int

	Stats      *stats.Registry
	InboundTag string

	Logger *logging.Logger
}

func Datagrams(ctx context.Context, config DatagramRelayConfig) error {
	if config.Logger == nil {
		config.Logger = logging.Discard()
	}

	defer config.ClientSide.Close()
	defer config.ClientSession.Close()

	stopClosingOnCancel := context.AfterFunc(ctx, func() {
		config.ClientSide.Close()
		config.ClientSession.Close()
	})
	defer stopClosingOnCancel()

	directionEnded := make(chan error, 2)
	go func() { directionEnded <- forwardClientDatagramsToSession(ctx, config) }()
	go func() { directionEnded <- forwardSessionRepliesToClient(config) }()

	firstError := <-directionEnded
	config.ClientSide.Close()
	config.ClientSession.Close()
	<-directionEnded

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return firstError
}

// A destination that cannot be routed, or a send that fails, loses that packet only.
func forwardClientDatagramsToSession(ctx context.Context, config DatagramRelayConfig) error {
	maxDatagramSize := config.MaxDatagramSize
	if maxDatagramSize <= 0 {
		maxDatagramSize = udp.DefaultMaxDatagramSize
	}
	clientPacket := make([]byte, maxDatagramSize)
	inboundCounters := config.Stats.Inbound(config.InboundTag)

	for {
		packetSize, destination, err := config.ClientSide.ReadFrom(clientPacket)
		if err != nil {
			if errors.Is(err, spec.ErrMessageTooLong) {
				inboundCounters.DatagramDroppedFor(stats.InboundDropTooLargeFromClient)
				config.Logger.Debug("Datagram dropped on read", "error", err)
				continue
			}
			return err
		}

		routed, err := config.OutboundForDestination(ctx, destination)
		if errors.Is(err, ErrDestinationBlocked) {
			inboundCounters.DatagramDroppedFor(stats.InboundDropBlockedByRule)
			continue
		}
		if err != nil {
			inboundCounters.DatagramDroppedFor(stats.InboundDropRoutingFailed)
			config.Logger.Debug("Datagram dropped, no outbound", "destination", destination, "error", err)
			continue
		}

		if err := config.ClientSession.Send(clientPacket[:packetSize], destination, routed.Outbound); err != nil {
			if reason, lostOnInboundSide := inboundDropReasonOfSendFailure(err); lostOnInboundSide {
				inboundCounters.DatagramDroppedFor(reason)
			}
			config.Logger.Debug("Datagram dropped on send", "destination", destination, "error", err)
			continue
		}
		if routed.UplinkAccepted != nil {
			routed.UplinkAccepted(packetSize)
		}
	}
}

func inboundDropReasonOfSendFailure(err error) (reason stats.InboundDatagramDropReason, lostOnInboundSide bool) {
	switch {
	case errors.Is(err, session.ErrOpeningOutbound), errors.Is(err, session.ErrSendingToOutbound):
		return 0, false
	case errors.Is(err, spec.ErrUnsupportedNetwork):
		return stats.InboundDropOutboundCannotCarryUDP, true
	case errors.Is(err, session.ErrClosed):
		return stats.InboundDropSessionEnded, true
	default:
		return stats.InboundDropOther, true
	}
}

func forwardSessionRepliesToClient(config DatagramRelayConfig) error {
	for {
		select {
		case reply := <-config.ClientSession.Replies():
			replySize := len(reply.Payload)
			if config.ReplyReceived != nil {
				config.ReplyReceived(reply.From, replySize)
			}
			_, err := config.ClientSide.WriteTo(reply.Payload, reply.From)
			reply.Release()
			if err != nil {
				config.Stats.Outbound(reply.OutboundTag).DatagramDroppedFor(stats.OutboundDropReplyNotDelivered)
				if errors.Is(err, spec.ErrMessageTooLong) {
					config.Logger.Debug("Reply dropped on write", "bytes", replySize, "error", err)
					continue
				}
				return err
			}
		case <-config.ClientSession.Done():
			return config.ClientSession.Err()
		}
	}
}
