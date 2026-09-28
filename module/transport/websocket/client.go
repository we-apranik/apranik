package websocket

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// A client that defers its handshake to the first Write has no context left
// to bound the wait for the server's answer, so it uses this. PROVISIONAL:
// the outbound's default handshake timeout.
const deferredHandshakeTimeout = 10 * time.Second

type client struct {
	path                string
	host                string
	extraHeaderLines    string
	maxEarlyData        int
	earlyDataHeaderName string
}

func newClient(settings *Settings) *client {
	// Sorted, so every request from one outbound has the same header order.
	names := make([]string, 0, len(settings.Headers))
	for name := range settings.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines strings.Builder
	hasUserAgent := false
	for _, name := range names {
		fmt.Fprintf(&lines, "%s: %s\r\n", name, settings.Headers[name])
		hasUserAgent = hasUserAgent || strings.EqualFold(name, "User-Agent")
	}
	if !hasUserAgent {
		fmt.Fprintf(&lines, "User-Agent: %s\r\n", defaultUserAgent)
	}

	return &client{
		path:                settings.Path,
		host:                settings.Host,
		extraHeaderLines:    lines.String(),
		maxEarlyData:        settings.MaxEarlyData,
		earlyDataHeaderName: settings.EarlyDataHeaderName,
	}
}

// clientHandshake is everything needed to send the upgrade request later.
type clientHandshake struct {
	client *client
	host   string
}

// Without early data the handshake completes here, within ctx. With it, the
// request waits for the first Write, which carries the start of the payload,
// as Xray and sing-box clients do.
func (c *client) WrapClientStream(ctx context.Context, underlyingStream spec.Stream, server spec.Destination) (spec.Stream, error) {
	host := c.host
	if host == "" {
		host = hostHeaderFor(server)
	}
	handshake := &clientHandshake{client: c, host: host}

	limiter, reader := newHandshakeReader(underlyingStream)
	wrapped := &stream{underlyingStream: underlyingStream, reader: reader, isClient: true}

	if c.maxEarlyData > 0 {
		wrapped.pendingHandshake = handshake
		wrapped.handshakeLimiter = limiter
		wrapped.handshakeFinished = make(chan struct{})
		return wrapped, nil
	}

	release := boundByContext(ctx, underlyingStream)
	err := handshake.run(underlyingStream, reader, nil)
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("websocket: handshake with %s: %w", server, err)
	}
	limiter.limited = false
	wrapped.handshakeDone.Store(true)
	return wrapped, nil
}

// The Host header carries the port only when it is not HTTP's default.
func hostHeaderFor(server spec.Destination) string {
	if server.Port() == 80 || server.Port() == 443 {
		if ip, isIP := server.IP(); isIP && ip.Is6() {
			return "[" + server.Host() + "]"
		}
		return server.Host()
	}
	return server.String()
}

// run sends the upgrade request, with earlyData in it if any, and checks the
// answer.
func (h *clientHandshake) run(underlyingStream spec.Stream, reader *bufio.Reader, earlyData []byte) error {
	key := newKey()
	path := h.client.path
	var earlyDataLine string
	if len(earlyData) > 0 {
		encoded := encodeEarlyData(earlyData)
		if h.client.earlyDataHeaderName == "" {
			path = pathWithEarlyData(path, encoded)
		} else {
			earlyDataLine = h.client.earlyDataHeaderName + ": " + encoded + "\r\n"
		}
	}

	request := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + h.host + "\r\n" +
		h.client.extraHeaderLines +
		earlyDataLine +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n\r\n"
	if _, err := underlyingStream.Write([]byte(request)); err != nil {
		return fmt.Errorf("sending the upgrade request: %w", err)
	}

	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		return fmt.Errorf("reading the upgrade response: %w", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("%w: %s", errUpgradeRefused, response.Status)
	}
	if !headerContainsToken(response.Header, "Upgrade", "websocket") || !headerContainsToken(response.Header, "Connection", "upgrade") {
		return fmt.Errorf("%w: missing Upgrade or Connection", errUpgradeRefused)
	}
	if response.Header.Get("Sec-WebSocket-Accept") != acceptKeyFor(key) {
		return errWrongAcceptKey
	}
	return nil
}

// sing-box's path mode appends early data to the path, before any query.
func pathWithEarlyData(path, encoded string) string {
	if pathOnly, query, hasQuery := strings.Cut(path, "?"); hasQuery {
		return pathOnly + encoded + "?" + query
	}
	return path + encoded
}

// completeHandshakeWithEarlyData runs a deferred handshake on the first
// Write, putting up to maxEarlyData bytes of p in it, and reports how many.
func (s *stream) completeHandshakeWithEarlyData(p []byte) (int, error) {
	if s.handshakeDone.Load() {
		return 0, nil
	}
	s.handshakeMutex.Lock()
	defer s.handshakeMutex.Unlock()
	if s.handshakeDone.Load() || s.handshakeErr != nil {
		return 0, s.handshakeErr
	}

	early := p[:min(len(p), s.pendingHandshake.client.maxEarlyData)]
	s.underlyingStream.SetDeadline(time.Now().Add(deferredHandshakeTimeout))
	err := s.pendingHandshake.run(s.underlyingStream, s.reader, early)
	s.underlyingStream.SetDeadline(time.Time{})
	if err != nil {
		s.finishHandshake(fmt.Errorf("websocket: handshake: %w", err))
		return 0, s.handshakeErr
	}
	s.handshakeLimiter.limited = false
	s.finishHandshake(nil)
	return len(early), nil
}

// finishHandshake records the outcome of a deferred handshake and wakes any
// Read waiting for it. The caller holds handshakeMutex.
func (s *stream) finishHandshake(err error) {
	if s.handshakeDone.Load() || s.handshakeErr != nil {
		return
	}
	s.handshakeErr = err
	s.pendingHandshake = nil
	if err == nil {
		s.handshakeDone.Store(true)
	}
	close(s.handshakeFinished)
}

// A Read before any Write waits for the Write to do the handshake, so the
// first payload still rides in it, as Xray and sing-box clients behave. A
// protocol that reads before it writes would wait forever; none of Apranik's
// does.
func (s *stream) completeHandshake() error {
	if s.handshakeDone.Load() {
		return nil
	}
	<-s.handshakeFinished
	s.handshakeMutex.Lock()
	defer s.handshakeMutex.Unlock()
	return s.handshakeErr
}
