package muxcool

import "errors"

var (
	ErrProtocol = errors.New("muxcool: protocol violation")

	ErrClosed = errors.New("muxcool: closed")

	ErrRemoteReset = errors.New("muxcool: sub-connection ended by the peer with an error")

	ErrDatagramTooLarge = errors.New("muxcool: datagram too large")

	ErrDestinationNotBound = errors.New("muxcool: destination is not the one this sub-connection is bound to")
)

var errIDsUsedUp = errors.New("muxcool: all 65535 sub-connection IDs of this main connection are used")
