// Package http is the HTTP proxy module: CONNECT tunnels and absolute-form
// requests on the server, CONNECT on the client. It speaks the protocol over
// whatever stream it is given and never opens a connection itself.
package http

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/shared/streamdeadline"
	"github.com/The-Apranik/Apranik/spec"
)

const statusBadRequest = "400 Bad Request"

type streamServer struct {
	username string
	password string
}

// CONNECT becomes a tunnel and returns the underlying stream unchanged; an absolute-form
// request is rewritten for the origin and replayed ahead of the connection.
//
// As with SOCKS5, the 200 goes out before core has dialled, so a dial failure
// reaches the client as an EOF rather than a 502.
func (s *streamServer) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	defer streamdeadline.ApplyContextDeadline(ctx, underlyingStream)()

	reader := wire.AcquireHeadReader(underlyingStream)
	defer wire.ReleaseHeadReader(reader)
	requestLine, headers, err := wire.ReadHead(reader)
	if err != nil {
		return nil, spec.Metadata{}, err
	}

	method, target, version, err := parseRequestLine(requestLine)
	if err != nil {
		writeStatus(underlyingStream, "HTTP/1.1", statusBadRequest)
		return nil, spec.Metadata{}, err
	}

	if err := s.authorize(underlyingStream, version, headers); err != nil {
		return nil, spec.Metadata{}, err
	}

	metadata := spec.Metadata{Network: spec.NetworkTCP, Source: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr()), User: s.username}

	if strings.EqualFold(method, "CONNECT") {
		return acceptConnect(underlyingStream, reader, metadata, version, target)
	}
	return acceptAbsoluteForm(underlyingStream, reader, metadata, version, method, target, headers)
}

func (s *streamServer) authorize(underlyingStream spec.Stream, version string, headers []string) error {
	if s.username == "" {
		return nil
	}
	offered := wire.HeaderValue(headers, "Proxy-Authorization")
	if wire.CredentialsMatch(offered, wire.BasicCredentials(s.username, s.password)) {
		return nil
	}
	writeStatus(underlyingStream, version, "407 Proxy Authentication Required", `Proxy-Authenticate: Basic realm="apranik"`)
	return errors.New("http: missing or bad proxy credentials")
}

func acceptConnect(underlyingStream spec.Stream, reader *bufio.Reader, metadata spec.Metadata, version, target string) (spec.Stream, spec.Metadata, error) {
	destination, err := wire.ParseAddress(target, 0)
	if err != nil {
		writeStatus(underlyingStream, version, statusBadRequest)
		return nil, spec.Metadata{}, err
	}
	if err := writeStatus(underlyingStream, version, "200 Connection established"); err != nil {
		return nil, spec.Metadata{}, fmt.Errorf("http: sending reply: %w", err)
	}
	metadata.Destination = destination
	return wire.PrefixStream(underlyingStream, nil, reader), metadata, nil
}

func acceptAbsoluteForm(underlyingStream spec.Stream, reader *bufio.Reader, metadata spec.Metadata, version, method, target string, headers []string) (spec.Stream, spec.Metadata, error) {
	absolute, err := url.Parse(target)
	if err != nil || absolute.Scheme == "" || absolute.Host == "" {
		writeStatus(underlyingStream, version, statusBadRequest)
		return nil, spec.Metadata{}, fmt.Errorf("http: %s %q is neither CONNECT nor an absolute-form request", method, target)
	}
	if !strings.EqualFold(absolute.Scheme, "http") {
		writeStatus(underlyingStream, version, statusBadRequest)
		return nil, spec.Metadata{}, fmt.Errorf("http: cannot forward scheme %q, use CONNECT", absolute.Scheme)
	}

	destination, err := wire.ParseAddress(absolute.Host, 80)
	if err != nil {
		writeStatus(underlyingStream, version, statusBadRequest)
		return nil, spec.Metadata{}, err
	}
	metadata.Destination = destination

	return wire.PrefixStream(underlyingStream, originForm(method, absolute, version, headers), reader), metadata, nil
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
// proxy must not forward. Connection: close is forced because the underlying
// stream becomes a tunnel to one origin, and a keep-alive request naming a
// different host would be delivered to the wrong one.
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

func writeStatus(underlyingStream spec.Stream, version, status string, headers ...string) error {
	var response bytes.Buffer
	fmt.Fprintf(&response, "%s %s\r\n", version, status)
	for _, header := range headers {
		response.WriteString(header)
		response.WriteString("\r\n")
	}
	response.WriteString("\r\n")

	_, err := underlyingStream.Write(response.Bytes())
	return err
}
