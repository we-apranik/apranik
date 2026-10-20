package websocket

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/we-apranik/apranik/shared/originalsourceip"
	"github.com/we-apranik/apranik/shared/streamdeadline"
	"github.com/we-apranik/apranik/shared/websocket/handshake"
	"github.com/we-apranik/apranik/spec"
)

type server struct {
	requiredHost        string
	path                string
	handshakeTimeout    time.Duration
	earlyDataHeaderName string
	earlyDataInPath     bool
	maxEarlyData        int
	originalSourceIP    *originalsourceip.Resolver // nil when disabled
}

func newServer(settings *Settings) *server {
	built := &server{
		requiredHost:        settings.Host,
		path:                settings.Path,
		handshakeTimeout:    settings.handshakeTimeout(),
		earlyDataHeaderName: defaultEarlyDataHeaderName,
		maxEarlyData:        maxEarlyDataLimit,
		originalSourceIP:    settings.OriginalSourceIP.NewResolver(),
	}
	if earlyData := settings.EarlyData; earlyData != nil {
		built.earlyDataHeaderName = earlyData.effectiveHeaderName()
		built.earlyDataInPath = earlyData.isInPath()
		built.maxEarlyData = earlyData.MaxBytes
	}
	return built
}

func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream, transport *spec.TransportMetadata) (spec.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	release := streamdeadline.BindToContext(ctx, underlyingStream)
	limiter, reader := handshake.NewReader(underlyingStream)
	earlyData, requestHeader, err := s.acceptUpgrade(underlyingStream, reader)
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("websocket: handshake with %s: %w", underlyingStream.RemoteAddr(), err)
	}
	limiter.LiftLimit()

	if s.originalSourceIP != nil {
		if source, believed := s.originalSourceIP.Resolve(requestHeader, transport.SourceEndpoint); believed {
			transport.SourceEndpoint = source
		}
	}

	wrapped := &stream{underlyingStream: underlyingStream, reader: reader, pendingEarlyData: earlyData}
	wrapped.handshakeDone.Store(true)
	return wrapped, nil
}

func (s *server) acceptUpgrade(underlyingStream spec.Stream, reader *bufio.Reader) ([]byte, http.Header, error) {
	request, err := http.ReadRequest(reader)
	if err != nil {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, nil, fmt.Errorf("reading the upgrade request: %w", err)
	}

	// 400 for a wrong host and 404 for a wrong path, as sing-box answers.
	if s.requiredHost != "" && !handshake.HostMatches(request.Host, s.requiredHost) {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, nil, fmt.Errorf("%w: %q", errWrongHost, request.Host)
	}

	earlyData, pathMatches, err := s.matchPath(request)
	if !pathMatches {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return nil, nil, fmt.Errorf("%w: %q", errWrongPath, request.URL.Path)
	}
	if err != nil {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, nil, err
	}

	key := request.Header.Get("Sec-WebSocket-Key")
	if key == "" || request.Method != http.MethodGet || !handshake.HeaderContainsToken(request.Header, "Upgrade", "websocket") || !handshake.HeaderContainsToken(request.Header, "Connection", "upgrade") || request.Header.Get("Sec-WebSocket-Version") != "13" {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, nil, errNotAnUpgrade
	}

	// Xray's rule: a header value that is valid base64 is early data, and is
	// echoed back, since a client that sent Sec-WebSocket-Protocol expects to
	// see it chosen. A value that is not base64 is ignored, as Xray does.
	var protocolEcho string
	if len(earlyData) == 0 {
		if value := request.Header.Get(s.earlyDataHeaderName); value != "" {
			if decoded, err := decodeEarlyData(value); err == nil && len(decoded) > 0 {
				earlyData = decoded
				if strings.EqualFold(s.earlyDataHeaderName, "Sec-WebSocket-Protocol") {
					protocolEcho = "Sec-WebSocket-Protocol: " + value + "\r\n"
				}
			}
		}
	}

	if len(earlyData) > s.maxEarlyData {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, nil, fmt.Errorf("%w: %d bytes, more than %d", errTooMuchEarlyData, len(earlyData), s.maxEarlyData)
	}

	response := "HTTP/1.1 101 Switching Protocols\r\n" + "Upgrade: websocket\r\n" + "Connection: Upgrade\r\n" + "Sec-WebSocket-Accept: " + acceptKeyFor(key) + "\r\n" + protocolEcho + "\r\n"
	if _, err := underlyingStream.Write([]byte(response)); err != nil {
		return nil, nil, fmt.Errorf("answering the upgrade: %w", err)
	}
	return earlyData, request.Header, nil
}

// matchPath compares the path without its query. In sing-box's path mode, the
// path may carry early data after the configured prefix.
func (s *server) matchPath(request *http.Request) (earlyData []byte, pathMatches bool, err error) {
	if request.URL.Path == s.path {
		return nil, true, nil
	}
	if !s.earlyDataInPath || !strings.HasPrefix(request.URL.Path, s.path) {
		return nil, false, nil
	}
	earlyData, err = decodeEarlyData(request.URL.Path[len(s.path):])
	if err != nil {
		return nil, true, fmt.Errorf("%w: %v", errBadEarlyData, err)
	}
	return earlyData, true, nil
}
