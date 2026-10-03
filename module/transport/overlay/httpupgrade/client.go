package httpupgrade

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/The-Apranik/Apranik/shared/streamdeadline"
	"github.com/The-Apranik/Apranik/shared/websocket/handshake"
	"github.com/The-Apranik/Apranik/spec"
)

// A pipelining client reads the 101 on its first Read, outside any context,
// so this bounds the wait. PROVISIONAL: the outbound's default handshake
// timeout.
const pipelinedResponseTimeout = 10 * time.Second

var errUpgradeRefused = errors.New("httpupgrade: the server did not switch protocols")

type client struct {
	path             string
	host             string
	extraHeaderLines string
	pipeline         bool
}

func newClient(settings *Settings) *client {
	return &client{
		path:             settings.Path,
		host:             settings.Host,
		extraHeaderLines: handshake.RequestHeaderLines(settings.Headers),
		pipeline:         settings.Pipeline,
	}
}

// The request carries exactly "Connection: Upgrade" and no
// Sec-WebSocket-Key: Xray's server compares the whole Connection value, and
// sing-box's refuses a key.
func (c *client) WrapClientStream(ctx context.Context, underlyingStream spec.Stream, server spec.Destination) (spec.Stream, error) {
	host := c.host
	if host == "" {
		host = handshake.HostHeaderFor(server)
	}
	request := "GET " + c.path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		c.extraHeaderLines +
		"Connection: Upgrade\r\n" +
		"Upgrade: websocket\r\n\r\n"

	release := streamdeadline.BindToContext(ctx, underlyingStream)
	limiter, reader := handshake.NewReader(underlyingStream)
	_, err := underlyingStream.Write([]byte(request))
	if err == nil && !c.pipeline {
		err = checkResponse(reader)
	}
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("httpupgrade: handshake with %s: %w", server, err)
	}

	wrapped := &stream{Stream: underlyingStream, handshakeReader: reader}
	if c.pipeline {
		wrapped.pendingResponse = &pendingResponse{limiter: limiter}
	} else {
		limiter.LiftLimit()
	}
	return wrapped, nil
}

// checkResponse accepts Upgrade and Connection as lists, which is more
// lenient than Xray and sing-box and loses nothing.
func checkResponse(reader *bufio.Reader) error {
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
	return nil
}

// pendingResponse is the 101 a pipelining client has not read yet.
type pendingResponse struct {
	limiter *handshake.LimitedReader
}

// Only the read deadline is touched: a Write may be in flight, and its own
// deadline is not this method's to change.
func (p *pendingResponse) read(underlyingStream spec.Stream, reader *bufio.Reader) error {
	underlyingStream.SetReadDeadline(time.Now().Add(pipelinedResponseTimeout))
	err := checkResponse(reader)
	underlyingStream.SetReadDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("httpupgrade: handshake: %w", err)
	}
	p.limiter.LiftLimit()
	return nil
}
