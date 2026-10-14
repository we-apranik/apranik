package websocket

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/we-apranik/apranik/shared/streamdeadline"
	"github.com/we-apranik/apranik/shared/websocket/handshake"
	"github.com/we-apranik/apranik/spec"
)

type client struct {
	path             string
	host             string
	extraHeaderLines string
	handshakeTimeout time.Duration

	// Zero sends no early data. An empty header name puts it in the path.
	maxEarlyData        int
	earlyDataHeaderName string
}

func newClient(settings *Settings) *client {
	built := &client{
		path:             settings.Path,
		host:             settings.Host,
		extraHeaderLines: handshake.RequestHeaderLines(settings.Headers),
		handshakeTimeout: settings.handshakeTimeout(),
	}
	if earlyData := settings.EarlyData; earlyData != nil {
		built.maxEarlyData = earlyData.MaxBytes
		if !earlyData.isInPath() {
			built.earlyDataHeaderName = earlyData.effectiveHeaderName()
		}
	}
	return built
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
		host = handshake.HostHeaderFor(server)
	}
	upgradeHandshake := &clientHandshake{client: c, host: host}

	limiter, reader := handshake.NewReader(underlyingStream)
	wrapped := &stream{underlyingStream: underlyingStream, reader: reader, isClient: true}

	if c.maxEarlyData > 0 {
		wrapped.pendingHandshake = upgradeHandshake
		wrapped.handshakeLimiter = limiter
		wrapped.handshakeFinished = make(chan struct{})
		wrapped.callerReadDeadlineChanged = make(chan struct{})
		return wrapped, nil
	}

	ctx, cancel := context.WithTimeout(ctx, c.handshakeTimeout)
	defer cancel()
	release := streamdeadline.BindToContext(ctx, underlyingStream)
	err := upgradeHandshake.sendUpgradeRequestAndCheckResponse(underlyingStream, reader, nil)
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("websocket: handshake with %s: %w", server, err)
	}
	limiter.LiftLimit()
	wrapped.handshakeDone.Store(true)
	return wrapped, nil
}

func (h *clientHandshake) sendUpgradeRequestAndCheckResponse(underlyingStream spec.Stream, reader *bufio.Reader, earlyData []byte) error {
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
	if !handshake.HeaderContainsToken(response.Header, "Upgrade", "websocket") ||
		!handshake.HeaderContainsToken(response.Header, "Connection", "upgrade") {
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
	s.setHandshakeDeadline(time.Now().Add(s.pendingHandshake.client.handshakeTimeout))
	err := s.pendingHandshake.sendUpgradeRequestAndCheckResponse(s.underlyingStream, s.reader, early)
	s.setHandshakeDeadline(time.Time{})
	if err != nil {
		s.finishHandshake(fmt.Errorf("websocket: handshake: %w", err))
		return 0, s.handshakeErr
	}
	s.handshakeLimiter.LiftLimit()
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
// first payload still rides in it, as Xray and sing-box clients behave. The
// wait obeys the caller's read deadline, including one set while waiting,
// as a read on the stream below would.
func (s *stream) completeHandshake() error {
	if s.handshakeDone.Load() {
		return nil
	}
	for {
		s.deadlinesMutex.Lock()
		readDeadline, readDeadlineChanged := s.callerReadDeadline, s.callerReadDeadlineChanged
		s.deadlinesMutex.Unlock()

		if !readDeadline.IsZero() && !time.Now().Before(readDeadline) {
			return os.ErrDeadlineExceeded
		}
		if finished, err := s.waitForHandshakeUntil(readDeadline, readDeadlineChanged); finished {
			return err
		}
	}
}

// waitForHandshakeUntil reports finished as false when the read deadline
// changed before the handshake ended, so the caller waits again with the new
// one.
func (s *stream) waitForHandshakeUntil(readDeadline time.Time, readDeadlineChanged <-chan struct{}) (finished bool, err error) {
	var readDeadlinePassed <-chan time.Time
	if !readDeadline.IsZero() {
		readDeadlineTimer := time.NewTimer(time.Until(readDeadline))
		defer readDeadlineTimer.Stop()
		readDeadlinePassed = readDeadlineTimer.C
	}
	select {
	case <-s.handshakeFinished:
		s.handshakeMutex.Lock()
		defer s.handshakeMutex.Unlock()
		return true, s.handshakeErr
	case <-readDeadlinePassed:
		return true, os.ErrDeadlineExceeded
	case <-readDeadlineChanged:
		return false, nil
	}
}

// The zero time ends the handshake's bound. Either way the caller's own
// deadlines stay in force: one set before the first Write, an idle bound for
// example, must outlive the handshake.
func (s *stream) setHandshakeDeadline(handshakeDeadline time.Time) {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.handshakeDeadline = handshakeDeadline
	s.applyDeadlinesLocked()
}

// The zero time means no deadline, so it is later than any other.
func earlierDeadline(first, second time.Time) time.Time {
	switch {
	case first.IsZero():
		return second
	case second.IsZero() || first.Before(second):
		return first
	default:
		return second
	}
}
