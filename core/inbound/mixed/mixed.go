// Package mixed is the special inbound protocol that serves SOCKS5 and HTTP
// on one port, choosing per connection from the first byte.
package mixed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/core/inbound"
	"github.com/The-Apranik/Apranik/core/net/peekable"
	"github.com/The-Apranik/Apranik/spec"
)

// Every SOCKS5 connection opens with its version byte; an HTTP request opens
// with a method, which is ASCII letters.
const socks5VersionByte = 0x05

// The result is an ordinary spec.StreamProxyServer, so fallback, sniffing and
// timeouts behave as for either protocol alone.
func NewStreamServer(socks5Server, httpServer spec.StreamProxyServer) spec.StreamProxyServer {
	return &streamServer{socks5Server: socks5Server, httpServer: httpServer}
}

type streamServer struct {
	socks5Server spec.StreamProxyServer
	httpServer   spec.StreamProxyServer
}

// Exactly one byte: on UDP ASSOCIATE core hands Associate its own carrier,
// not the peekable one made here, so nothing may stay buffered in it.
func (s *streamServer) AcceptStream(ctx context.Context, carrier spec.Stream) (spec.Stream, spec.Metadata, error) {
	peekableCarrier := peekable.NewStream(carrier)
	if deadline, ok := ctx.Deadline(); ok {
		carrier.SetReadDeadline(deadline)
	}
	firstByte, err := peekableCarrier.PeekExactly(1)
	carrier.SetReadDeadline(time.Time{})
	if err != nil {
		return nil, spec.Metadata{}, fmt.Errorf("mixed: reading the first byte: %w", err)
	}

	if firstByte[0] == socks5VersionByte {
		return s.socks5Server.AcceptStream(ctx, peekableCarrier)
	}
	return s.httpServer.AcceptStream(ctx, peekableCarrier)
}

func (s *streamServer) SetAssociateSocket(socket spec.UDPAssociateSocketFunc) {
	if associateHandler, ok := s.socks5Server.(inbound.UDPAssociateHandler); ok {
		associateHandler.SetAssociateSocket(socket)
	}
}

var errSOCKS5HasNoAssociate = errors.New("mixed: the socks5 server does not implement UDP ASSOCIATE")

func (s *streamServer) Associate(ctx context.Context, controlStream spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	associateHandler, ok := s.socks5Server.(inbound.UDPAssociateHandler)
	if !ok {
		return nil, errSOCKS5HasNoAssociate
	}
	return associateHandler.Associate(ctx, controlStream, metadata)
}
