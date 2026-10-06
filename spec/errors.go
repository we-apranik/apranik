package spec

import "errors"

var (
	ErrNoHalfClose             = errors.New("spec: carrier has no half-close")
	ErrMessageTooLong          = errors.New("spec: message too large for carrier")
	ErrUnsupportedNetwork      = errors.New("spec: network not supported here")
	ErrUDPAssociationRequested = errors.New("spec: hand off to a UDP associate handler")
	ErrSniffNeedMoreData       = errors.New("spec: sniffer needs more data")

	// ErrDNSAnswerTruncated: a datagram answer had its truncation flag set and
	// the server cannot retry over a stream.
	ErrDNSAnswerTruncated = errors.New("spec: dns answer truncated")

	// ErrFallbackRequested is returned by Accept* for a request that is not
	// valid for this protocol. Core replays every byte the handshake read, and
	// the rest of the stream, to the inbound's fallback. Return it only before
	// writing anything to the carrier.
	ErrFallbackRequested = errors.New("spec: not this protocol's request; hand the stream to the fallback")

	// ErrAuthenticationFailed is wrapped by Accept* when the request was
	// valid for this protocol but its credentials were not accepted. Core
	// counts it, so an operator sees a wave of guessed passwords apart from
	// scanners that sent garbage; a malformed request is not this error. It
	// may be returned together with ErrFallbackRequested.
	ErrAuthenticationFailed = errors.New("spec: credentials were not accepted")
)
