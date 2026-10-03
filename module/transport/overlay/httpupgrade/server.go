package httpupgrade

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/The-Apranik/Apranik/shared/streamdeadline"
	"github.com/The-Apranik/Apranik/shared/websocket/handshake"
	"github.com/The-Apranik/Apranik/spec"
)

var (
	errWrongHost           = errors.New("httpupgrade: wrong host")
	errWrongPath           = errors.New("httpupgrade: wrong path")
	errNotAnUpgrade        = errors.New("httpupgrade: not an upgrade request")
	errWebSocketKeyRefused = errors.New("httpupgrade: a WebSocket key was sent and accept_websocket_key is off")
)

// Exactly these bytes: Xray's client compares the whole status line and the
// whole Connection value.
const switchingProtocolsResponse = "HTTP/1.1 101 Switching Protocols\r\n" + "Connection: Upgrade\r\n" + "Upgrade: websocket\r\n\r\n"

type server struct {
	requiredHost       string
	path               string
	acceptWebSocketKey bool
}

func newServer(settings *Settings) *server {
	return &server{
		requiredHost:       settings.Host,
		path:               settings.Path,
		acceptWebSocketKey: settings.AcceptWebSocketKey,
	}
}

// The handshake reader is kept: a pipelining client's first payload may
// already sit in it, behind the request. Xray's and sing-box's servers drop
// those bytes; this one hands them on.
func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, error) {
	release := streamdeadline.BindToContext(ctx, underlyingStream)
	limiter, reader := handshake.NewReader(underlyingStream)
	err := s.acceptUpgrade(underlyingStream, reader)
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("httpupgrade: handshake with %s: %w", underlyingStream.RemoteAddr(), err)
	}
	limiter.LiftLimit()
	return &stream{Stream: underlyingStream, handshakeReader: reader}, nil
}

// Refusals answer as sing-box does: 400 for a wrong host or a malformed
// request, 404 for everything else.
func (s *server) acceptUpgrade(underlyingStream spec.Stream, reader *bufio.Reader) error {
	request, err := http.ReadRequest(reader)
	if err != nil {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return fmt.Errorf("reading the upgrade request: %w", err)
	}
	if s.requiredHost != "" && !handshake.HostMatches(request.Host, s.requiredHost) {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return fmt.Errorf("%w: %q", errWrongHost, request.Host)
	}
	if request.URL.Path != s.path {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return fmt.Errorf("%w: %q", errWrongPath, request.URL.Path)
	}
	if request.Method != http.MethodGet ||
		!handshake.HeaderContainsToken(request.Header, "Connection", "upgrade") ||
		!handshake.HeaderContainsToken(request.Header, "Upgrade", "websocket") {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return errNotAnUpgrade
	}
	if !s.acceptWebSocketKey && request.Header.Get("Sec-WebSocket-Key") != "" {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return errWebSocketKeyRefused
	}
	if _, err := underlyingStream.Write([]byte(switchingProtocolsResponse)); err != nil {
		return fmt.Errorf("answering the upgrade: %w", err)
	}
	return nil
}
