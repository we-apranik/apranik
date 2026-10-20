// Package mixed is the special inbound protocol that serves SOCKS5 and HTTP
// on one port, choosing per connection from the first byte.
package mixed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/core/net/peekable"
	"github.com/we-apranik/apranik/spec"
)

const socks5VersionByte = 0x05

// PROVISIONAL.
const DefaultFirstByteTimeout = 10 * time.Second

func NewStreamServer(socks5Server, httpServer spec.StreamProxyServer, firstByteTimeout time.Duration) spec.StreamProxyServer {
	if firstByteTimeout == 0 {
		firstByteTimeout = DefaultFirstByteTimeout
	}
	server := &streamServer{socks5Server: socks5Server, httpServer: httpServer, firstByteTimeout: firstByteTimeout}

	socks5Manager, socks5Manages := socks5Server.(spec.UserManager)
	httpManager, httpManages := httpServer.(spec.UserManager)
	if socks5Manages && httpManages {
		return &userManagingStreamServer{streamServer: server, socks5Manager: socks5Manager, httpManager: httpManager}
	}
	return server
}

type streamServer struct {
	socks5Server     spec.StreamProxyServer
	httpServer       spec.StreamProxyServer
	firstByteTimeout time.Duration
}

var _ spec.UDPAssociateHandler = (*streamServer)(nil)

func (s *streamServer) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	peekableStream := peekable.NewStream(underlyingStream)
	underlyingStream.SetReadDeadline(time.Now().Add(s.firstByteTimeout))
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
