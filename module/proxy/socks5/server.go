// Package socks5 is the SOCKS5 proxy module: RFC 1928 CONNECT and UDP
// ASSOCIATE, with RFC 1929 username/password. ASSOCIATE needs a second, UDP
// socket; the module never opens one itself but asks core for it through
// spec.UDPAssociateSocketFunc.
package socks5

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/module/proxy/socks5/wire"
	"github.com/we-apranik/apranik/shared/socksaddr"
	"github.com/we-apranik/apranik/shared/streamdeadline"
	"github.com/we-apranik/apranik/spec"
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
	// (users.go), so a handshake reads it with one atomic load and never sees a
	// list half changed. No users means no authentication.
	users       atomic.Pointer[[]user]
	changeUsers sync.Mutex

	udpRelay bool

	handshakeTimeout time.Duration

	// The largest payload an association takes from a client.
	maxDatagramSize int

	socket spec.UDPAssociateSocketFunc
}

var _ spec.UDPAssociateHandler = (*streamServer)(nil)

// Without a socket func, UDP ASSOCIATE is refused with X'07'.
func (s *streamServer) SetAssociateSocket(socket spec.UDPAssociateSocketFunc) { s.socket = socket }

// The CONNECT reply goes out before core has dialled, so a dial failure
// reaches the client as an EOF rather than X'04'. That is the cost of
// "wrapper, not pump."
//
// UDP ASSOCIATE returns a nil Stream with Network NetworkUDP, since
// AcceptStream cannot return a Datagram; Associate binds and replies.
func (s *streamServer) AcceptStream(ctx context.Context, underlyingStream spec.Stream) (spec.Stream, spec.Metadata, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	defer streamdeadline.BindToContext(ctx, underlyingStream)()

	authenticatedUser, err := s.authenticate(underlyingStream)
	if err != nil {
		return nil, spec.Metadata{}, err
	}

	var request [3]byte
	if _, err := io.ReadFull(underlyingStream, request[:]); err != nil {
		return nil, spec.Metadata{}, fmt.Errorf("socks5: reading request: %w", err)
	}
	if request[0] != wire.Version5 {
		return nil, spec.Metadata{}, fmt.Errorf("socks5: request version X'%02X', want X'05'", request[0])
	}
	command := request[1]

	metadata := spec.Metadata{Transport: spec.TransportMetadata{SourceEndpoint: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr())}, User: authenticatedUser}

	switch {
	case command == wire.CommandConnect:
		destination, err := socksaddr.ReadAddress(underlyingStream)
		if err != nil {
			wire.WriteReply(underlyingStream, wire.ReplyAddressNotSupported, spec.Endpoint{})
			return nil, spec.Metadata{}, err
		}
		if err := wire.WriteReply(underlyingStream, wire.ReplySucceeded, spec.Endpoint{}); err != nil {
			return nil, spec.Metadata{}, fmt.Errorf("socks5: sending reply: %w", err)
		}
		metadata.Network = spec.NetworkTCP
		metadata.Destination = destination
		return underlyingStream, metadata, nil

	case command == wire.CommandUDPAssociate && s.udpRelay && s.socket != nil:
		metadata.Network = spec.NetworkUDP
		// Inform core that we need an UDP association
		return nil, metadata, spec.ErrUDPAssociationRequested

	default:
		if err := wire.WriteReply(underlyingStream, wire.ReplyCommandNotSupported, spec.Endpoint{}); err != nil {
			return nil, spec.Metadata{}, fmt.Errorf("socks5: refusing %s: %w", wire.CommandText(command), err)
		}
		return nil, spec.Metadata{}, fmt.Errorf("socks5: %s not supported here", wire.CommandText(command))
	}
}

// Reads the declared address AcceptStream left unread, opens the socket, and
// replies. Does not wait for the session: core owns the relay from here.
func (s *streamServer) Associate(ctx context.Context, controlStream spec.Stream, metadata spec.Metadata) (spec.Datagram, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	defer streamdeadline.BindToContext(ctx, controlStream)()

	declaredClient, err := socksaddr.ReadAddress(controlStream)
	if err != nil {
		wire.WriteReply(controlStream, wire.ReplyAddressNotSupported, spec.Endpoint{})
		return nil, err
	}

	socket, err := s.socket(ctx)
	if err != nil {
		wire.WriteReply(controlStream, wire.ReplyGeneralFailure, spec.Endpoint{})
		return nil, fmt.Errorf("socks5: binding a datagram socket: %w", err)
	}

	relayAddress, err := reachableAddress(socket.LocalAddr(), controlStream.LocalAddr())
	if err != nil {
		socket.Close()
		wire.WriteReply(controlStream, wire.ReplyGeneralFailure, spec.Endpoint{})
		return nil, err
	}
	if err := wire.WriteReply(controlStream, wire.ReplySucceeded, relayAddress); err != nil {
		socket.Close()
		return nil, fmt.Errorf("socks5: sending reply: %w", err)
	}

	return newAssociation(socket, declaredClient, s.maxDatagramSize), nil
}

// A wildcard bind is useless to the client, so the control stream supplies the
// host and the bound socket the port.
func reachableAddress(bound, control net.Addr) (spec.Endpoint, error) {
	boundEndpoint := spec.NewEndpointFromNetAddr(bound)
	if !boundEndpoint.IsValid() {
		return spec.Endpoint{}, fmt.Errorf("socks5: bound address %q is not an IP and port", bound)
	}

	if !boundEndpoint.Addr().IsUnspecified() {
		return boundEndpoint, nil
	}

	controlEndpoint := spec.NewEndpointFromNetAddr(control)
	if !controlEndpoint.IsValid() {
		return spec.Endpoint{}, errors.New("socks5: cannot tell the client which address to send to")
	}
	return spec.NewEndpoint(controlEndpoint.Addr(), boundEndpoint.Port())
}

// authenticate returns the name of the user who logged in, empty when the
// server has no users and so asks for no login.
func (s *streamServer) authenticate(underlyingStream spec.Stream) (string, error) {
	users := *s.users.Load()

	var greeting [2]byte
	if _, err := io.ReadFull(underlyingStream, greeting[:]); err != nil {
		return "", fmt.Errorf("socks5: reading greeting: %w", err)
	}
	if greeting[0] != wire.Version5 {
		return "", fmt.Errorf("socks5: greeting version X'%02X', want X'05'", greeting[0])
	}
	if greeting[1] == 0 {
		return "", errors.New("socks5: client offered no authentication methods")
	}

	offered := make([]byte, greeting[1])
	if _, err := io.ReadFull(underlyingStream, offered); err != nil {
		return "", fmt.Errorf("socks5: reading authentication methods: %w", err)
	}

	required := byte(wire.MethodNoAuth)
	if len(users) > 0 {
		required = wire.MethodUserPass
	}
	if !slices.Contains(offered, required) {
		underlyingStream.Write([]byte{wire.Version5, wire.MethodNone})
		return "", fmt.Errorf("socks5: client does not offer authentication method X'%02X'", required)
	}
	if _, err := underlyingStream.Write([]byte{wire.Version5, required}); err != nil {
		return "", fmt.Errorf("socks5: selecting authentication method: %w", err)
	}
	if required == wire.MethodNoAuth {
		return "", nil
	}
	return s.checkCredentials(underlyingStream, users)
}

func (s *streamServer) checkCredentials(underlyingStream spec.Stream, users []user) (string, error) {
	var versionAndUsernameLength [2]byte
	if _, err := io.ReadFull(underlyingStream, versionAndUsernameLength[:]); err != nil {
		return "", fmt.Errorf("socks5: reading credentials: %w", err)
	}
	if versionAndUsernameLength[0] != wire.AuthVersion {
		return "", fmt.Errorf("socks5: credential version X'%02X', want X'01'", versionAndUsernameLength[0])
	}

	username, err := readLengthPrefixed(underlyingStream, versionAndUsernameLength[1], "username")
	if err != nil {
		return "", err
	}
	var passwordLength [1]byte
	if _, err := io.ReadFull(underlyingStream, passwordLength[:]); err != nil {
		return "", fmt.Errorf("socks5: reading password length: %w", err)
	}
	password, err := readLengthPrefixed(underlyingStream, passwordLength[0], "password")
	if err != nil {
		return "", err
	}

	// Every user is compared, with both fields, before deciding, so the time
	// taken says nothing about which user matched, how many there are before
	// it, or whether the username or the password was the wrong part.
	matchedIndex, found := 0, 0
	for index, candidate := range users {
		match := subtle.ConstantTimeCompare(username, candidate.nameBytes) & subtle.ConstantTimeCompare(password, candidate.passwordBytes)
		matchedIndex = subtle.ConstantTimeSelect(match, index, matchedIndex)
		found |= match
	}
	if found == 0 {
		underlyingStream.Write([]byte{wire.AuthVersion, wire.AuthFailed})
		return "", fmt.Errorf("%w: socks5: bad username or password", spec.ErrAuthenticationFailed)
	}

	if _, err := underlyingStream.Write([]byte{wire.AuthVersion, wire.AuthSucceeded}); err != nil {
		return "", fmt.Errorf("socks5: accepting credentials: %w", err)
	}
	return users[matchedIndex].name, nil
}

func readLengthPrefixed(underlyingStream spec.Stream, length byte, fieldName string) ([]byte, error) {
	field := make([]byte, length)
	if _, err := io.ReadFull(underlyingStream, field); err != nil {
		return nil, fmt.Errorf("socks5: reading %s: %w", fieldName, err)
	}
	return field, nil
}
