package httpupgrade

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/we-apranik/apranik/shared/originalsourceip"
	"github.com/we-apranik/apranik/shared/streamdeadline"
	"github.com/we-apranik/apranik/shared/websocket/handshake"
	"github.com/we-apranik/apranik/spec"
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
	handshakeTimeout   time.Duration
	originalSourceIP   *originalsourceip.Resolver // nil when disabled
}

func newServer(settings *Settings) *server {
	return &server{
		requiredHost:       settings.Host,
		path:               settings.Path,
		acceptWebSocketKey: settings.AcceptWebSocketKey,
		handshakeTimeout:   settings.handshakeTimeout(),
		originalSourceIP:   settings.OriginalSourceIP.NewResolver(),
	}
}

// The handshake reader is kept: a pipelining client's first payload may
// already sit in it, behind the request. Xray's and sing-box's servers drop
// those bytes; this one hands them on.
func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream, transport *spec.TransportMetadata) (spec.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	release := streamdeadline.BindToContext(ctx, underlyingStream)
	limiter, reader := handshake.NewReader(underlyingStream)
	requestHeader, err := s.acceptUpgrade(underlyingStream, reader)
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("httpupgrade: handshake with %s: %w", underlyingStream.RemoteAddr(), err)
	}
	limiter.LiftLimit()
	if s.originalSourceIP != nil {
		if source, believed := s.originalSourceIP.Resolve(requestHeader, transport.SourceEndpoint); believed {
			transport.SourceEndpoint = source
		}
	}
	return &stream{Stream: underlyingStream, handshakeReader: reader}, nil
}

// Refusals answer as sing-box does: 400 for a wrong host or a malformed
// request, 404 for everything else.
func (s *server) acceptUpgrade(underlyingStream spec.Stream, reader *bufio.Reader) (http.Header, error) {
	request, err := http.ReadRequest(reader)
	if err != nil {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, fmt.Errorf("reading the upgrade request: %w", err)
	}
	if s.requiredHost != "" && !handshake.HostMatches(request.Host, s.requiredHost) {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, fmt.Errorf("%w: %q", errWrongHost, request.Host)
	}
	if request.URL.Path != s.path {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return nil, fmt.Errorf("%w: %q", errWrongPath, request.URL.Path)
	}
	if request.Method != http.MethodGet ||
		!handshake.HeaderContainsToken(request.Header, "Connection", "upgrade") ||
		!handshake.HeaderContainsToken(request.Header, "Upgrade", "websocket") {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return nil, errNotAnUpgrade
	}
	if !s.acceptWebSocketKey && request.Header.Get("Sec-WebSocket-Key") != "" {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return nil, errWebSocketKeyRefused
	}
	if _, err := underlyingStream.Write([]byte(switchingProtocolsResponse)); err != nil {
		return nil, fmt.Errorf("answering the upgrade: %w", err)
	}
	return request.Header, nil
}
