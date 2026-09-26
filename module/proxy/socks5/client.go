package socks5

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/socks5/wire"
	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

type streamClient struct {
	username string
	password string
}

// RequestStream speaks the client half of the handshake over a carrier that
// already reaches the server. SOCKS5 round-trips, so an auth failure or a
// refused destination surfaces here rather than on the first read.
func (c *streamClient) RequestStream(ctx context.Context, carrier spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("socks5: %w: this client speaks CONNECT only", spec.ErrUnsupportedNetwork)
	}

	// Encoded first so a destination this protocol cannot express fails before
	// anything reaches the wire.
	request, err := socksaddr.AppendAddress([]byte{wire.Version5, wire.CommandConnect, 0x00}, metadata.Destination)
	if err != nil {
		return nil, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		carrier.SetDeadline(deadline)
		defer carrier.SetDeadline(time.Time{})
	}

	offered := byte(wire.MethodNoAuth)
	if c.username != "" {
		offered = wire.MethodUserPass
	}
	if _, err := carrier.Write([]byte{wire.Version5, 1, offered}); err != nil {
		return nil, fmt.Errorf("socks5: sending greeting: %w", err)
	}

	var selection [2]byte
	if _, err := io.ReadFull(carrier, selection[:]); err != nil {
		return nil, fmt.Errorf("socks5: reading method selection: %w", err)
	}
	if selection[0] != wire.Version5 {
		return nil, fmt.Errorf("socks5: method selection version X'%02X', want X'05'", selection[0])
	}
	switch selection[1] {
	case offered:
	case wire.MethodNone:
		return nil, fmt.Errorf("socks5: server accepted none of the methods offered")
	default:
		return nil, fmt.Errorf("socks5: server chose method X'%02X', we offered X'%02X'", selection[1], offered)
	}

	if offered == wire.MethodUserPass {
		if err := c.sendCredentials(carrier); err != nil {
			return nil, err
		}
	}

	if _, err := carrier.Write(request); err != nil {
		return nil, fmt.Errorf("socks5: sending request: %w", err)
	}

	var reply [3]byte
	if _, err := io.ReadFull(carrier, reply[:]); err != nil {
		return nil, fmt.Errorf("socks5: reading reply: %w", err)
	}
	if reply[0] != wire.Version5 {
		return nil, fmt.Errorf("socks5: reply version X'%02X', want X'05'", reply[0])
	}
	if reply[1] != wire.ReplySucceeded {
		return nil, &wire.ReplyError{Code: reply[1]}
	}
	// The bound address: read to keep the stream aligned, then discarded.
	if _, err := socksaddr.ReadAddress(carrier); err != nil {
		return nil, fmt.Errorf("socks5: reading bound address: %w", err)
	}

	return carrier, nil
}

func (c *streamClient) sendCredentials(carrier spec.Stream) error {
	message := []byte{wire.AuthVersion, byte(len(c.username))}
	message = append(message, c.username...)
	message = append(message, byte(len(c.password)))
	message = append(message, c.password...)
	if _, err := carrier.Write(message); err != nil {
		return fmt.Errorf("socks5: sending credentials: %w", err)
	}

	var status [2]byte
	if _, err := io.ReadFull(carrier, status[:]); err != nil {
		return fmt.Errorf("socks5: reading credential status: %w", err)
	}
	if status[0] != wire.AuthVersion {
		return fmt.Errorf("socks5: credential status version X'%02X', want X'01'", status[0])
	}
	if status[1] != wire.AuthSucceeded {
		return fmt.Errorf("socks5: server rejected the credentials")
	}
	return nil
}
