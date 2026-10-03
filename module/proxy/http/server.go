// Package http is the HTTP proxy module: CONNECT tunnels and absolute-form
// requests on the server, CONNECT on the client. It speaks the protocol over
// whatever stream it is given and never opens a connection itself.
package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/shared/streamdeadline"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	statusConnectionEstablished       = "200 Connection established"
	statusBadRequest                  = "400 Bad Request"
	statusProxyAuthenticationRequired = "407 Proxy Authentication Required"
)

type streamServer struct {
	username string
	password string
}

// A CONNECT request becomes a tunnel (server_connect.go); an absolute-form
// request is rewritten for the origin (server_absolute_form.go).
func (s *streamServer) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	defer streamdeadline.BindToContext(ctx, underlyingStream)()

	head, streamAfterHead, err := wire.ReadHead(underlyingStream)
	if err != nil {
		return nil, spec.Metadata{}, err
	}

	requestMethod, requestTarget, httpVersion, err := parseRequestLine(head.StartLine)
	if err != nil {
		writeStatusResponse(underlyingStream, "HTTP/1.1", statusBadRequest)
		return nil, spec.Metadata{}, err
	}

	if err := s.requireProxyCredentials(underlyingStream, httpVersion, head); err != nil {
		return nil, spec.Metadata{}, err
	}

	metadata := spec.Metadata{Network: spec.NetworkTCP, Source: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr()), User: s.username}

	if strings.EqualFold(requestMethod, "CONNECT") {
		return acceptConnectTunnel(streamAfterHead, metadata, httpVersion, requestTarget)
	}
	return acceptAbsoluteFormRequest(streamAfterHead, metadata, httpVersion, requestMethod, requestTarget, head.HeaderLines)
}

func (s *streamServer) requireProxyCredentials(underlyingStream spec.Stream, httpVersion string, head wire.Head) error {
	if s.username == "" {
		return nil
	}
	if wire.BasicCredentialsMatch(head.HeaderValue("Proxy-Authorization"), s.username, s.password) {
		return nil
	}
	writeStatusResponse(underlyingStream, httpVersion, statusProxyAuthenticationRequired, `Proxy-Authenticate: Basic realm="apranik"`)
	return errors.New("http: missing or bad proxy credentials")
}

func parseRequestLine(requestLine string) (requestMethod, requestTarget, httpVersion string, err error) {
	fields := strings.Fields(requestLine)
	if len(fields) != 3 {
		return "", "", "", fmt.Errorf("http: malformed request line %q", requestLine)
	}
	switch fields[2] {
	case "HTTP/1.0", "HTTP/1.1":
	default:
		return "", "", "", fmt.Errorf("http: unsupported version %q", fields[2])
	}
	return fields[0], fields[1], fields[2], nil
}

func writeStatusResponse(underlyingStream spec.Stream, httpVersion, status string, headerLines ...string) error {
	var response bytes.Buffer
	fmt.Fprintf(&response, "%s %s\r\n", httpVersion, status)
	for _, headerLine := range headerLines {
		response.WriteString(headerLine)
		response.WriteString("\r\n")
	}
	response.WriteString("\r\n")

	_, err := underlyingStream.Write(response.Bytes())
	return err
}
