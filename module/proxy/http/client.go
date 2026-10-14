package http

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/we-apranik/apranik/module/proxy/http/wire"
	"github.com/we-apranik/apranik/shared/streamdeadline"
	"github.com/we-apranik/apranik/spec"
)

// ConnectRefusedError is a CONNECT the upstream proxy answered with a status
// other than 2xx.
type ConnectRefusedError struct {
	StatusCode int
	StatusLine string
}

func (e *ConnectRefusedError) Error() string {
	return fmt.Sprintf("http: proxy answered CONNECT with %q", e.StatusLine)
}

type streamClient struct {
	username         string
	password         string
	handshakeTimeout time.Duration
}

// RequestStream issues CONNECT and nothing else. Forwarding an absolute-form
// request would mean parsing and rewriting the payload, which an outbound that
// is handed an opaque stream has no business doing.
func (c *streamClient) RequestStream(ctx context.Context, underlyingStream spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("http: %w: CONNECT carries TCP only", spec.ErrUnsupportedNetwork)
	}

	connectTarget, err := wire.FormatHostPort(metadata.Destination)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.handshakeTimeout)
	defer cancel()
	defer streamdeadline.BindToContext(ctx, underlyingStream)()

	var connectRequest bytes.Buffer
	fmt.Fprintf(&connectRequest, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", connectTarget, connectTarget)
	if c.username != "" {
		fmt.Fprintf(&connectRequest, "Proxy-Authorization: %s\r\n", wire.BasicAuthorizationValue(c.username, c.password))
	}
	connectRequest.WriteString("\r\n")

	if _, err := underlyingStream.Write(connectRequest.Bytes()); err != nil {
		return nil, fmt.Errorf("http: sending CONNECT: %w", err)
	}

	head, streamAfterHead, err := wire.ReadHead(underlyingStream)
	if err != nil {
		return nil, err
	}

	statusCode, err := parseConnectStatusCode(head.StartLine)
	if err != nil {
		return nil, err
	}
	if statusCode < 200 || statusCode > 299 {
		return nil, &ConnectRefusedError{StatusCode: statusCode, StatusLine: head.StartLine}
	}

	return streamAfterHead, nil
}

func parseConnectStatusCode(statusLine string) (int, error) {
	fields := strings.SplitN(statusLine, " ", 3)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0, fmt.Errorf("http: malformed status line %q", statusLine)
	}

	statusCode, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("http: malformed status code in %q", statusLine)
	}
	return statusCode, nil
}
