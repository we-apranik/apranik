package vless

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/vless/wire"
	"github.com/The-Apranik/Apranik/spec"
)

var (
	// A request from a known user asking for a flow (XTLS Vision), which this
	// module does not implement. The client is genuine, so this is no fallback.
	ErrUnsupportedFlow = errors.New("vless: flow not supported")

	// A request from a known user for a command this module does not implement
	// (Mux, which carries XUDP, or Reverse).
	ErrUnsupportedCommand = errors.New("vless: command not supported")
)

type user struct {
	name string
	uuid wire.UUID
}

func newUser(configured User) (user, error) {
	uuid, err := wire.ParseUUID(configured.UUID.Reveal())
	if err != nil {
		return user{}, fmt.Errorf("vless: user %q: %w", configured.Name, err)
	}
	return user{name: configured.Name, uuid: uuid}, nil
}

// The users are an immutable snapshot, replaced whole by AddUser and
// RemoveUser (users.go), so a handshake reads them with one atomic load and no
// lock, and never sees a list half changed.
type server struct {
	users            atomic.Pointer[[]user]
	changeUsers      sync.Mutex
	handshakeTimeout time.Duration
}

func newServer(users []user, handshakeTimeout time.Duration) *server {
	s := &server{handshakeTimeout: handshakeTimeout}
	s.users.Store(&users)
	return s
}

// Only the addons, whose length the request gives, sit before the command byte,
// so the kind of request is known without reading, or judging, the rest.
func (s *server) IsDatagramOverStream(ctx context.Context, underlyingStream spec.PeekableStream) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		underlyingStream.SetReadDeadline(deadline)
		defer underlyingStream.SetReadDeadline(time.Time{})
	}

	prefix, err := underlyingStream.Peek(wire.RequestPrefixLength)
	if err != nil {
		return false, fmt.Errorf("vless: reading request: %w", err)
	}
	commandOffset, err := wire.CommandOffset(prefix)
	if err != nil {
		return false, err
	}
	withCommand, err := underlyingStream.Peek(commandOffset + 1)
	if err != nil {
		return false, fmt.Errorf("vless: reading request: %w", err)
	}
	return withCommand[commandOffset] == wire.CommandUDP, nil
}

// Nothing is written during the handshake, so every refusal may ask for the
// fallback.
func (s *server) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	_, metadata, err := s.acceptRequest(ctx, underlyingStream, wire.CommandTCP)
	if err != nil {
		return nil, spec.Metadata{}, err
	}
	return newResponseHeaderFirstStream(underlyingStream), metadata, nil
}

func (s *server) AcceptDatagramOverStream(ctx context.Context, underlyingStream spec.Stream) (spec.Datagram, spec.Metadata, error) {
	request, metadata, err := s.acceptRequest(ctx, underlyingStream, wire.CommandUDP)
	if err != nil {
		return nil, spec.Metadata{}, err
	}
	return newServerDatagram(underlyingStream, request.Destination), metadata, nil
}

func (s *server) acceptRequest(ctx context.Context, underlyingStream spec.Stream, wantedCommand byte) (wire.Request, spec.Metadata, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		underlyingStream.SetReadDeadline(deadline)
		defer underlyingStream.SetReadDeadline(time.Time{})
	}

	request, err := wire.ReadRequest(underlyingStream)
	if err != nil {
		if isMalformedRequest(err) {
			return wire.Request{}, spec.Metadata{}, fmt.Errorf("%w: %w", spec.ErrFallbackRequested, err)
		}
		return wire.Request{}, spec.Metadata{}, err
	}

	name, found := s.lookUp(request.UUID)
	if !found {
		return wire.Request{}, spec.Metadata{}, fmt.Errorf("%w: %w: vless: unknown uuid", spec.ErrAuthenticationFailed, spec.ErrFallbackRequested)
	}

	switch {
	case request.Flow != "":
		return wire.Request{}, spec.Metadata{}, fmt.Errorf("%w: %q", ErrUnsupportedFlow, request.Flow)
	case request.Command == wire.CommandMux || request.Command == wire.CommandReverse:
		return wire.Request{}, spec.Metadata{}, fmt.Errorf("%w: X'%02X'", ErrUnsupportedCommand, request.Command)
	case request.Command != wantedCommand:
		// Core dispatched on IsDatagramOverStream, so this is a mismatch
		// between the two calls, not a client error.
		return wire.Request{}, spec.Metadata{}, fmt.Errorf("vless: command X'%02X' reached the wrong accept: %w", request.Command, spec.ErrUnsupportedNetwork)
	}

	metadata := spec.Metadata{Transport: spec.TransportMetadata{SourceEndpoint: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr())}, User: name}
	if request.Command == wire.CommandTCP {
		metadata.Network = spec.NetworkTCP
		metadata.Destination = request.Destination
	} else {
		metadata.Network = spec.NetworkUDP
	}
	return request, metadata, nil
}

// Every user is compared, match or not, so the time taken says nothing about
// which one matched or how many there are before it.
func (s *server) lookUp(uuid wire.UUID) (string, bool) {
	users := *s.users.Load()
	matchedIndex, found := 0, 0
	for index, candidate := range users {
		match := subtle.ConstantTimeCompare(uuid[:], candidate.uuid[:])
		matchedIndex = subtle.ConstantTimeSelect(match, index, matchedIndex)
		found |= match
	}
	if found == 0 {
		return "", false
	}
	return users[matchedIndex].name, true
}

// Only bytes that are not a VLESS request go to the fallback. A closed, reset
// or silent connection is closed.
func isMalformedRequest(err error) bool {
	return errors.Is(err, wire.ErrUnknownVersion) || errors.Is(err, wire.ErrUnknownCommand) ||
		errors.Is(err, wire.ErrMalformedAddons) || errors.Is(err, wire.ErrUnsupportedAddressType) ||
		errors.Is(err, wire.ErrEmptyDomain)
}
