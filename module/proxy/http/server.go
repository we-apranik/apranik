// Package http is the HTTP proxy module: CONNECT tunnels and absolute-form
// requests on the server, CONNECT on the client. It speaks the protocol over
// whatever carrier it is given and never opens a connection itself.
package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/spec"
)

type streamServer struct {
	username string
	password string
}

// CONNECT becomes a tunnel and returns the carrier unchanged; an absolute-form
// request is rewritten for the origin and replayed ahead of the connection.
//
// As with SOCKS5, the 200 goes out before core has dialled, so a dial failure
// reaches the client as an EOF rather than a 502.
func (s *streamServer) AcceptStream(ctx context.Context, carrier spec.Stream) (spec.Stream, spec.Metadata, error) {
	if deadline, ok := ctx.Deadline(); ok {
		carrier.SetDeadline(deadline)
		defer carrier.SetDeadline(time.Time{})
	}

	reader := wire.AcquireHeadReader(carrier)
	defer wire.ReleaseHeadReader(reader)
	requestLine, headers, err := wire.ReadHead(reader)
	if err != nil {
		return nil, spec.Metadata{}, err
	}

	method, target, version, err := parseRequestLine(requestLine)
	if err != nil {
		writeStatus(carrier, "HTTP/1.1", "400 Bad Request")
		return nil, spec.Metadata{}, err
	}

	if s.username != "" {
		offered := wire.HeaderValue(headers, "Proxy-Authorization")
		if !wire.CredentialsMatch(offered, wire.BasicCredentials(s.username, s.password)) {
			writeStatus(carrier, version, "407 Proxy Authentication Required", `Proxy-Authenticate: Basic realm="apranik"`)
			return nil, spec.Metadata{}, errors.New("http: missing or bad proxy credentials")
		}
	}

	metadata := spec.Metadata{Network: spec.NetworkTCP, Source: spec.NewEndpointFromNetAddr(carrier.RemoteAddr()), User: s.username}

	if strings.EqualFold(method, "CONNECT") {
		destination, err := wire.ParseAddress(target, 0)
		if err != nil {
			writeStatus(carrier, version, "400 Bad Request")
			return nil, spec.Metadata{}, err
		}
		if err := writeStatus(carrier, version, "200 Connection established"); err != nil {
			return nil, spec.Metadata{}, fmt.Errorf("http: sending reply: %w", err)
		}
		metadata.Destination = destination
		return wire.PrefixStream(carrier, nil, reader), metadata, nil
	}

	absolute, err := url.Parse(target)
	if err != nil || absolute.Scheme == "" || absolute.Host == "" {
		writeStatus(carrier, version, "400 Bad Request")
		return nil, spec.Metadata{}, fmt.Errorf("http: %s %q is neither CONNECT nor an absolute-form request", method, target)
	}
	if !strings.EqualFold(absolute.Scheme, "http") {
		writeStatus(carrier, version, "400 Bad Request")
		return nil, spec.Metadata{}, fmt.Errorf("http: cannot forward scheme %q, use CONNECT", absolute.Scheme)
	}

	destination, err := wire.ParseAddress(absolute.Host, 80)
	if err != nil {
		writeStatus(carrier, version, "400 Bad Request")
		return nil, spec.Metadata{}, err
	}
	metadata.Destination = destination

	return wire.PrefixStream(carrier, originForm(method, absolute, version, headers), reader), metadata, nil
}

func parseRequestLine(line string) (method, target, version string, err error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", "", "", fmt.Errorf("http: malformed request line %q", line)
	}
	switch fields[2] {
	case "HTTP/1.0", "HTTP/1.1":
	default:
		return "", "", "", fmt.Errorf("http: unsupported version %q", fields[2])
	}
	return fields[0], fields[1], fields[2], nil
}

// Rewrites the request line for the origin and drops the hop-by-hop headers a
// proxy must not forward. Connection: close is forced because the carrier
// becomes a tunnel to one origin, and a keep-alive request naming a different
// host would be delivered to the wrong one.
func originForm(method string, absolute *url.URL, version string, headers []string) []byte {
	var forwarded bytes.Buffer
	fmt.Fprintf(&forwarded, "%s %s %s\r\n", method, absolute.RequestURI(), version)

	for _, header := range headers {
		switch strings.ToLower(wire.HeaderName(header)) {
		case "proxy-connection", "proxy-authorization", "connection", "keep-alive":
			continue
		}
		forwarded.WriteString(header)
		forwarded.WriteString("\r\n")
	}

	forwarded.WriteString("Connection: close\r\n\r\n")
	return forwarded.Bytes()
}

func writeStatus(carrier spec.Stream, version, status string, headers ...string) error {
	var response bytes.Buffer
	fmt.Fprintf(&response, "%s %s\r\n", version, status)
	for _, header := range headers {
		response.WriteString(header)
		response.WriteString("\r\n")
	}
	response.WriteString("\r\n")

	_, err := carrier.Write(response.Bytes())
	return err
}
