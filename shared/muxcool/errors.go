package muxcool

import "errors"

var (
	// ErrProtocol is a frame, or a sequence of frames, that the format does not
	// allow. On a main connection it closes the connection.
	ErrProtocol = errors.New("muxcool: protocol violation")

	// ErrClosed is an operation on a main connection or a sub-connection that
	// is closed, or a main connection closed for being idle.
	ErrClosed = errors.New("muxcool: closed")

	// ErrSubConnectionLimit is an attempt to open a sub-connection past MaxConcurrentSubConnections,
	// MaxTotalSubConnections, or the 65535 IDs one main connection has.
	// The caller uses another main connection.
	ErrSubConnectionLimit = errors.New("muxcool: no room for another sub-connection on this main connection")

	// ErrRemoteReset is a sub-connection the peer ended with the error bit set.
	ErrRemoteReset = errors.New("muxcool: sub-connection ended by the peer with an error")

	// ErrDatagramTooLarge is a datagram bigger than the buffer given to ReadFrom,
	// or bigger than a frame can carry. The datagram is dropped and the
	// sub-connection keeps working.
	ErrDatagramTooLarge = errors.New("muxcool: datagram too large")

	// ErrDestinationNotBound is a write to a destination other than the one the
	// UDP sub-connection was opened for.
	ErrDestinationNotBound = errors.New("muxcool: destination is not the one this sub-connection is bound to")
)
