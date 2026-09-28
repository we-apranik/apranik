package websocket

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/The-Apranik/Apranik/shared/websocket/handshake"
	"github.com/The-Apranik/Apranik/spec"
)

type server struct {
	requiredHost        string
	path                string
	earlyDataHeaderName string
	earlyDataInPath     bool
}

func newServer(settings *Settings) *server {
	return &server{
		requiredHost:        settings.Host,
		path:                settings.Path,
		earlyDataHeaderName: settings.EarlyDataHeaderName,
		earlyDataInPath:     settings.EarlyDataHeaderName == "" && settings.MaxEarlyData > 0,
	}
}

func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, error) {
	release := handshake.BoundByContext(ctx, underlyingStream)
	limiter, reader := handshake.NewReader(underlyingStream)
	earlyData, err := s.acceptUpgrade(underlyingStream, reader)
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("websocket: handshake with %s: %w", underlyingStream.RemoteAddr(), err)
	}
	limiter.LiftLimit()

	wrapped := &stream{underlyingStream: underlyingStream, reader: reader, pendingEarlyData: earlyData}
	wrapped.handshakeDone.Store(true)
	return wrapped, nil
}

// acceptUpgrade reads and checks the request, answers it, and returns any
// early data it carried. A request it refuses gets an ordinary HTTP answer.
func (s *server) acceptUpgrade(underlyingStream spec.Stream, reader *bufio.Reader) ([]byte, error) {
	request, err := http.ReadRequest(reader)
	if err != nil {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, fmt.Errorf("reading the upgrade request: %w", err)
	}

	// 400 for a wrong host and 404 for a wrong path, as sing-box answers.
	if s.requiredHost != "" && !handshake.HostMatches(request.Host, s.requiredHost) {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, fmt.Errorf("%w: %q", errWrongHost, request.Host)
	}

	earlyData, pathMatches, err := s.matchPath(request)
	if !pathMatches {
		handshake.WriteRefusal(underlyingStream, http.StatusNotFound)
		return nil, fmt.Errorf("%w: %q", errWrongPath, request.URL.Path)
	}
	if err != nil {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, err
	}

	key := request.Header.Get("Sec-WebSocket-Key")
	if key == "" || request.Method != http.MethodGet || !handshake.HeaderContainsToken(request.Header, "Upgrade", "websocket") || !handshake.HeaderContainsToken(request.Header, "Connection", "upgrade") || request.Header.Get("Sec-WebSocket-Version") != "13" {
		handshake.WriteRefusal(underlyingStream, http.StatusBadRequest)
		return nil, errNotAnUpgrade
	}

	// Xray's rule: a header value that is valid base64 is early data, and is
	// echoed back, since a client that sent Sec-WebSocket-Protocol expects to
	// see it chosen. A value that is not base64 is ignored, as Xray does.
	var protocolEcho string
	if !s.earlyDataInPath && s.earlyDataHeaderName != "" {
		if value := request.Header.Get(s.earlyDataHeaderName); value != "" {
			if decoded, err := decodeEarlyData(value); err == nil && len(decoded) > 0 {
				earlyData = decoded
				if strings.EqualFold(s.earlyDataHeaderName, "Sec-WebSocket-Protocol") {
					protocolEcho = "Sec-WebSocket-Protocol: " + value + "\r\n"
				}
			}
		}
	}

	response := "HTTP/1.1 101 Switching Protocols\r\n" + "Upgrade: websocket\r\n" + "Connection: Upgrade\r\n" + "Sec-WebSocket-Accept: " + acceptKeyFor(key) + "\r\n" + protocolEcho + "\r\n"
	if _, err := underlyingStream.Write([]byte(response)); err != nil {
		return nil, fmt.Errorf("answering the upgrade: %w", err)
	}
	return earlyData, nil
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
