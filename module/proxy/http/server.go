// Package http is the HTTP proxy module: CONNECT tunnels and absolute-form
// requests on the server, CONNECT on the client. It speaks the protocol over
// whatever stream it is given and never opens a connection itself.
package http

import (
	"bytes"
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/shared/streamdeadline"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	statusConnectionEstablished       = "200 Connection established"
	statusBadRequest                  = "400 Bad Request"
	statusProxyAuthenticationRequired = "407 Proxy Authentication Required"
)

type user struct {
	name          string
	nameBytes     []byte
	passwordBytes []byte
}

func newUser(configured User) user {
	return user{name: configured.Name, nameBytes: []byte(configured.Name), passwordBytes: []byte(configured.Password.Reveal())}
}

type streamServer struct {
	// An immutable snapshot, replaced whole by AddUser and RemoveUser
	// (users.go), so a request reads it with one atomic load and never sees a
	// list half changed. No users means no authentication.
	users       atomic.Pointer[[]user]
	changeUsers sync.Mutex

	handshakeTimeout time.Duration
}

// A CONNECT request becomes a tunnel (server_connect.go); an absolute-form
// request is rewritten for the origin (server_absolute_form.go).
func (s *streamServer) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
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

	authenticatedUser, err := s.requireProxyCredentials(underlyingStream, httpVersion, head)
	if err != nil {
		return nil, spec.Metadata{}, err
	}

	metadata := spec.Metadata{Network: spec.NetworkTCP, Transport: spec.TransportMetadata{SourceEndpoint: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr())}, User: authenticatedUser}

	if strings.EqualFold(requestMethod, "CONNECT") {
		return acceptConnectTunnel(streamAfterHead, metadata, httpVersion, requestTarget)
	}
	return acceptAbsoluteFormRequest(streamAfterHead, metadata, httpVersion, requestMethod, requestTarget, head.HeaderLines)
}

// requireProxyCredentials returns the name of the user who logged in, empty
// when the server has no users and so asks for no login.
func (s *streamServer) requireProxyCredentials(underlyingStream spec.Stream, httpVersion string, head wire.Head) (string, error) {
	users := *s.users.Load()
	if len(users) == 0 {
		return "", nil
	}

	username, password, ok := wire.BasicCredentials(head.HeaderValue("Proxy-Authorization"))

	// Every user is compared, with both fields, before deciding, so the time
	// taken says nothing about which user matched, how many there are before
	// it, or whether the username or the password was the wrong part.
	matchedIndex, found := 0, 0
	for index, candidate := range users {
		match := subtle.ConstantTimeCompare(username, candidate.nameBytes) & subtle.ConstantTimeCompare(password, candidate.passwordBytes)
		matchedIndex = subtle.ConstantTimeSelect(match, index, matchedIndex)
		found |= match
	}
	if ok && found == 1 {
		return users[matchedIndex].name, nil
	}

	writeStatusResponse(underlyingStream, httpVersion, statusProxyAuthenticationRequired, `Proxy-Authenticate: Basic realm="apranik"`)
	return "", fmt.Errorf("%w: http: missing or bad proxy credentials", spec.ErrAuthenticationFailed)
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
