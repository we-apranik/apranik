package relay

import (
	"context"
	"errors"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp"
	"github.com/The-Apranik/Apranik/core/net/udp/session"
	"github.com/The-Apranik/Apranik/spec"
)

type DatagramRelayConfig struct {
	ClientSide    spec.Datagram
	ClientSession *session.Session

	// Asked for every packet: UDP has no connection to pin a decision to.
	OutboundForDestination func(ctx context.Context, destination spec.Destination) (spec.Outbound, error)

	// Nil is allowed: Datagrams substitutes a silent logger.
	Logger *logging.Logger
}

// Datagrams is the UDP half of the relay: client packets go to the session
// through whichever outbound OutboundForDestination names, and the session's
// replies go back to the client. It decides nothing itself.
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

	// The first direction to end ends the pair: closing both is what unblocks
	// the other loop.
	firstError := <-directionEnded
	config.ClientSide.Close()
	config.ClientSession.Close()
	<-directionEnded

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return firstError
}

// A destination that cannot be routed, or a send that fails, loses that packet
// only. Neither ends the session.
func forwardClientDatagramsToSession(ctx context.Context, config DatagramRelayConfig) error {
	clientPacket := make([]byte, udp.MaxDatagramSize)

	for {
		packetSize, destination, err := config.ClientSide.ReadFrom(clientPacket)
		if err != nil {
			if errors.Is(err, spec.ErrMessageTooLong) {
				config.Logger.Debug("Datagram dropped on read", "error", err)
				continue
			}
			return err
		}

		outbound, err := config.OutboundForDestination(ctx, destination)
		if err != nil {
			config.Logger.Debug("Datagram dropped, no outbound", "destination", destination, "error", err)
			continue
		}

		if err := config.ClientSession.Send(clientPacket[:packetSize], destination, outbound); err != nil {
			config.Logger.Debug("Datagram dropped on send", "destination", destination, "error", err)
			continue
		}
	}
}

func forwardSessionRepliesToClient(config DatagramRelayConfig) error {
	for {
		select {
		case reply := <-config.ClientSession.Replies():
			_, err := config.ClientSide.WriteTo(reply.Payload, reply.From)
			reply.Release()
			if err != nil {
				if errors.Is(err, spec.ErrMessageTooLong) {
					config.Logger.Debug("Reply dropped on write", "bytes", len(reply.Payload), "error", err)
					continue
				}
				return err
			}
		case <-config.ClientSession.Done():
			return config.ClientSession.Err()
		}
	}
}
