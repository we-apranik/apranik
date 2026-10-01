package http

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/shared/streamdeadline"
	"github.com/The-Apranik/Apranik/spec"
)

type streamClient struct {
	username string
	password string
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

	defer streamdeadline.ApplyContextDeadline(ctx, underlyingStream)()

	var request bytes.Buffer
	fmt.Fprintf(&request, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", connectTarget, connectTarget)
	if c.username != "" {
		fmt.Fprintf(&request, "Proxy-Authorization: %s\r\n", wire.BasicAuthorizationValue(c.username, c.password))
	}
	request.WriteString("\r\n")

	if _, err := underlyingStream.Write(request.Bytes()); err != nil {
		return nil, fmt.Errorf("http: sending CONNECT: %w", err)
	}

	headReader := wire.AcquireHeadReader(underlyingStream)
	defer wire.ReleaseHeadReader(headReader)
	statusLine, _, err := wire.ReadMessageHead(headReader)
	if err != nil {
		return nil, err
	}

	code, err := parseConnectStatusCode(statusLine)
	if err != nil {
		return nil, err
	}
	if code < 200 || code > 299 {
		return nil, &wire.ConnectRefusedError{StatusCode: code, StatusLine: statusLine}
	}

	return wire.ReplayStream(underlyingStream, nil, headReader), nil
}

func parseConnectStatusCode(line string) (int, error) {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0, fmt.Errorf("http: malformed status line %q", line)
	}

	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("http: malformed status code in %q", line)
	}
	return code, nil
}
