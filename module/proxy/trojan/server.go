// Package trojan is the Trojan proxy module: a password-hash header, then raw
// payload, with no server reply. CONNECT gives a stream; UDP ASSOCIATE gives
// datagrams framed inside the same stream.
package trojan

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/trojan/wire"
	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

type user struct {
	name         string
	passwordHash wire.PasswordHash
}

type server struct {
	users            []user
	handshakeTimeout time.Duration
}

// The command byte sits at a fixed offset, so the kind of request is known
// without reading, or judging, the rest.
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

// Nothing is written during the handshake, so every refusal may ask for the
// fallback.
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
		return spec.Metadata{}, fmt.Errorf("%w: trojan: unknown password", spec.ErrFallbackRequested)
	}

	metadata := spec.Metadata{Transport: spec.TransportMetadata{SourceEndpoint: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr())}, User: name}
	switch {
	case request.Command != wantedCommand:
		// Core dispatched on IsDatagramOverStream, so this is a mismatch
		// between the two calls, not a client error.
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
	matchedIndex, found := 0, 0
	for index, candidate := range s.users {
		match := subtle.ConstantTimeCompare(hash[:], candidate.passwordHash[:])
		matchedIndex = subtle.ConstantTimeSelect(match, index, matchedIndex)
		found |= match
	}
	if found == 0 {
		return "", false
	}
	return s.users[matchedIndex].name, true
}

// Only bytes that are not a Trojan request go to the fallback. A closed,
// reset or silent connection is closed.
func isMalformedRequest(err error) bool {
	return errors.Is(err, wire.ErrMalformedPasswordHash) || errors.Is(err, wire.ErrMissingCRLF) ||
		errors.Is(err, wire.ErrUnknownCommand) || errors.Is(err, socksaddr.ErrUnsupportedAddressType) ||
		errors.Is(err, socksaddr.ErrEmptyDomain)
}
