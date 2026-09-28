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
	"time"

	"github.com/The-Apranik/Apranik/module/proxy/socks5/wire"
	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

type streamServer struct {
	username string
	password string
	udpRelay bool

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
	if deadline, ok := ctx.Deadline(); ok {
		underlyingStream.SetDeadline(deadline)
		defer underlyingStream.SetDeadline(time.Time{})
	}

	if err := s.authenticate(underlyingStream); err != nil {
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

	metadata := spec.Metadata{Source: spec.NewEndpointFromNetAddr(underlyingStream.RemoteAddr()), User: s.username}

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
	if deadline, ok := ctx.Deadline(); ok {
		controlStream.SetDeadline(deadline)
		defer controlStream.SetDeadline(time.Time{})
	}

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

	return newAssociation(socket, declaredClient), nil
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

func (s *streamServer) authenticate(underlyingStream spec.Stream) error {
	var greeting [2]byte
	if _, err := io.ReadFull(underlyingStream, greeting[:]); err != nil {
		return fmt.Errorf("socks5: reading greeting: %w", err)
	}
	if greeting[0] != wire.Version5 {
		return fmt.Errorf("socks5: greeting version X'%02X', want X'05'", greeting[0])
	}
	if greeting[1] == 0 {
		return errors.New("socks5: client offered no authentication methods")
	}

	offered := make([]byte, greeting[1])
	if _, err := io.ReadFull(underlyingStream, offered); err != nil {
		return fmt.Errorf("socks5: reading authentication methods: %w", err)
	}

	required := byte(wire.MethodNoAuth)
	if s.username != "" {
		required = wire.MethodUserPass
	}
	if !slices.Contains(offered, required) {
		underlyingStream.Write([]byte{wire.Version5, wire.MethodNone})
		return fmt.Errorf("socks5: client does not offer authentication method X'%02X'", required)
	}
	if _, err := underlyingStream.Write([]byte{wire.Version5, required}); err != nil {
		return fmt.Errorf("socks5: selecting authentication method: %w", err)
	}
	if required == wire.MethodNoAuth {
		return nil
	}
	return s.checkCredentials(underlyingStream)
}

func (s *streamServer) checkCredentials(underlyingStream spec.Stream) error {
	var header [2]byte
	if _, err := io.ReadFull(underlyingStream, header[:]); err != nil {
		return fmt.Errorf("socks5: reading credentials: %w", err)
	}
	if header[0] != wire.AuthVersion {
		return fmt.Errorf("socks5: credential version X'%02X', want X'01'", header[0])
	}

	username := make([]byte, header[1])
	if _, err := io.ReadFull(underlyingStream, username); err != nil {
		return fmt.Errorf("socks5: reading username: %w", err)
	}

	var passwordLength [1]byte
	if _, err := io.ReadFull(underlyingStream, passwordLength[:]); err != nil {
		return fmt.Errorf("socks5: reading password length: %w", err)
	}
	password := make([]byte, passwordLength[0])
	if _, err := io.ReadFull(underlyingStream, password); err != nil {
		return fmt.Errorf("socks5: reading password: %w", err)
	}

	// Both compared before deciding, so a wrong username and a wrong password
	// take the same time.
	usernameMatches := subtle.ConstantTimeCompare(username, []byte(s.username)) == 1
	passwordMatches := subtle.ConstantTimeCompare(password, []byte(s.password)) == 1
	if !usernameMatches || !passwordMatches {
		underlyingStream.Write([]byte{wire.AuthVersion, wire.AuthFailed})
		return errors.New("socks5: bad username or password")
	}

	if _, err := underlyingStream.Write([]byte{wire.AuthVersion, wire.AuthSucceeded}); err != nil {
		return fmt.Errorf("socks5: accepting credentials: %w", err)
	}
	return nil
}
