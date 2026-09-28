// Package mixed is the special inbound protocol that serves SOCKS5 and HTTP
// on one port, choosing per connection from the first byte.
package mixed

import (
	"context"
	"errors"
	"fmt"
	"time"

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

var _ spec.UDPAssociateHandler = (*streamServer)(nil)

// Exactly one byte: on UDP ASSOCIATE core hands Associate its own carrier,
// not the peekable one made here, so nothing may stay buffered in it.
func (s *streamServer) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	peekableStream := peekable.NewStream(underlyingStream)
	if deadline, ok := ctx.Deadline(); ok {
		underlyingStream.SetReadDeadline(deadline)
	}
	firstByte, err := peekableStream.PeekExactly(1)
	underlyingStream.SetReadDeadline(time.Time{})
	if err != nil {
		return nil, spec.Metadata{}, fmt.Errorf("mixed: reading the first byte: %w", err)
	}

	if firstByte[0] == socks5VersionByte {
		return s.socks5Server.AcceptStream(ctx, peekableStream)
	}
	return s.httpServer.AcceptStream(ctx, peekableStream)
}

func (s *streamServer) SetAssociateSocket(socket spec.UDPAssociateSocketFunc) {
	if associateHandler, ok := s.socks5Server.(spec.UDPAssociateHandler); ok {
		associateHandler.SetAssociateSocket(socket)
	}
}

var errSOCKS5HasNoAssociate = errors.New("mixed: the socks5 server does not implement UDP ASSOCIATE")

func (s *streamServer) Associate(ctx context.Context, controlStream spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	associateHandler, ok := s.socks5Server.(spec.UDPAssociateHandler)
	if !ok {
		return nil, errSOCKS5HasNoAssociate
	}
	return associateHandler.Associate(ctx, controlStream, metadata)
}
