package http

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/spec"
)

type streamClient struct {
	username string
	password string
}

// RequestStream issues CONNECT and nothing else. Forwarding an absolute-form
// request would mean parsing and rewriting the payload, which an outbound that
// is handed an opaque carrier has no business doing.
func (c *streamClient) RequestStream(ctx context.Context, carrier spec.Stream, metadata spec.Metadata) (spec.Stream, error) {
	if metadata.Network == spec.NetworkUDP {
		return nil, fmt.Errorf("http: %w: CONNECT carries TCP only", spec.ErrUnsupportedNetwork)
	}

	target, err := wire.FormatAddress(metadata.Destination)
	if err != nil {
		return nil, err
	}

	if deadline, ok := ctx.Deadline(); ok {
		carrier.SetDeadline(deadline)
		defer carrier.SetDeadline(time.Time{})
	}

	var request bytes.Buffer
	fmt.Fprintf(&request, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if c.username != "" {
		fmt.Fprintf(&request, "Proxy-Authorization: %s\r\n", wire.BasicCredentials(c.username, c.password))
	}
	request.WriteString("\r\n")

	if _, err := carrier.Write(request.Bytes()); err != nil {
		return nil, fmt.Errorf("http: sending CONNECT: %w", err)
	}

	reader := wire.AcquireHeadReader(carrier)
	defer wire.ReleaseHeadReader(reader)
	statusLine, _, err := wire.ReadHead(reader)
	if err != nil {
		return nil, err
	}

	code, err := parseStatusCode(statusLine)
	if err != nil {
		return nil, err
	}
	if code < 200 || code > 299 {
		return nil, &wire.StatusError{Code: code, Status: statusLine}
	}

	return wire.PrefixStream(carrier, nil, reader), nil
}

func parseStatusCode(line string) (int, error) {
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
