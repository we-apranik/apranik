// Package trojan is the Trojan proxy module: a password-hash header, then raw
// payload, with no server reply. CONNECT gives a stream; UDP ASSOCIATE gives
// datagrams framed inside the same stream.
package trojan

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/shared/socksaddr"
	"github.com/we-apranik/apranik/spec"
)

type user struct {
	name         string
	passwordHash wire.PasswordHash
}

func newUser(configured User) user {
	return user{name: configured.Name, passwordHash: wire.HashPassword(configured.Password.Reveal())}
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

const commandOffset = wire.PasswordHashLength + 2

func (s *server) IsDatagramOverStream(ctx context.Context, underlyingStream spec.PeekableStream) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		underlyingStream.SetReadDeadline(deadline)
		defer underlyingStream.SetReadDeadline(time.Time{})
	}

	prefix, err := underlyingStream.Peek(commandOffset + 1)
	if err != nil {
		return false, fmt.Errorf("trojan: reading request: %w", err)
	}
	return prefix[commandOffset] == wire.CommandUDPAssociate, nil
}

func (s *server) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	metadata, err := s.accept(ctx, underlyingStream, wire.CommandConnect)
	if err != nil {
		return nil, spec.Metadata{}, err
	}
	return underlyingStream, metadata, nil
}

func (s *server) AcceptDatagramOverStream(ctx context.Context, underlyingStream spec.Stream) (spec.Datagram, spec.Metadata, error) {
	metadata, err := s.accept(ctx, underlyingStream, wire.CommandUDPAssociate)
	if err != nil {
		return nil, spec.Metadata{}, err
	}
	return newStreamDatagram(underlyingStream), metadata, nil
}

func (s *server) accept(ctx context.Context, underlyingStream spec.Stream, wantedCommand byte) (spec.Metadata, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	if deadline, ok := ctx.Deadline(); ok {
		underlyingStream.SetReadDeadline(deadline)
		defer underlyingStream.SetReadDeadline(time.Time{})
	}

	request, err := wire.ReadRequest(underlyingStream)
	if err != nil {
		if isMalformedRequest(err) {
			return spec.Metadata{}, fmt.Errorf("%w: %w", spec.ErrFallbackRequested, err)
		}
		return spec.Metadata{}, err
	}

	name, found := s.lookUp(request.PasswordHash)
	if !found {
		return spec.Metadata{}, fmt.Errorf("%w: %w: trojan: unknown password", spec.ErrAuthenticationFailed, spec.ErrFallbackRequested)
	}

	metadata := spec.Metadata{Transport: spec.TransportMetadata{SourceEndpoint: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr())}, User: name}
	switch {
	case request.Command != wantedCommand:
		return spec.Metadata{}, fmt.Errorf("trojan: command X'%02X' reached the wrong accept: %w", request.Command, spec.ErrUnsupportedNetwork)
	case request.Command == wire.CommandConnect:
		metadata.Network = spec.NetworkTCP
		metadata.Destination = request.Destination
	default:
		metadata.Network = spec.NetworkUDP
	}
	return metadata, nil
}

// Every user is compared, match or not, so the time taken says nothing about
// which one matched or how many there are before it.
func (s *server) lookUp(hash wire.PasswordHash) (string, bool) {
	users := *s.users.Load()
	matchedIndex, found := 0, 0
	for index, candidate := range users {
		match := subtle.ConstantTimeCompare(hash[:], candidate.passwordHash[:])
		matchedIndex = subtle.ConstantTimeSelect(match, index, matchedIndex)
		found |= match
	}
	if found == 0 {
		return "", false
	}
	return users[matchedIndex].name, true
}

func isMalformedRequest(err error) bool {
	return errors.Is(err, wire.ErrMalformedPasswordHash) || errors.Is(err, wire.ErrMissingCRLF) ||
		errors.Is(err, wire.ErrUnknownCommand) || errors.Is(err, socksaddr.ErrUnsupportedAddressType) ||
		errors.Is(err, socksaddr.ErrEmptyDomain)
}
