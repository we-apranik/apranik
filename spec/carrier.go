package spec

import (
	"io"
	"net"
	"time"
)

// Stream is ordered, reliable bytes. net.Conn plus CloseWrite.
type Stream interface {
	io.ReadWriteCloser

	// CloseWrite says "I'm done sending" without closing the read side.
	// Carriers that can't express this return ErrNoHalfClose.
	CloseWrite() error

	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// PeekableStream is a Stream whose next bytes can be looked at without
// consuming them. Core implements it; modules receive it.
type PeekableStream interface {
	Stream

	// Peek returns the next n bytes without consuming them; fewer only with an
	// error. It blocks until they arrive or the read deadline passes, may read
	// ahead of n, and refuses an n larger than the implementation's buffer.
	Peek(n int) ([]byte, error)

	// PeekExactly is Peek that never reads past n, so once those bytes are
	// read nothing stays buffered and the underlying stream can be handed on.
	PeekExactly(n int) ([]byte, error)
}

// Datagram is messages with boundaries. Unordered, unreliable.
type Datagram interface {
	ReadFrom(p []byte) (n int, from Destination, err error)
	WriteTo(p []byte, to Destination) (n int, err error)
	Close() error
	LocalAddr() net.Addr
}

// ResolvedWriter is an optional capability on a Datagram. A carrier that
// resolves a domain itself reports the concrete address the write went to, so
// a layer above can match a reply -- which always arrives from an IP -- against
// what was actually sent to. Carriers that never resolve do not implement it,
// and a caller must work without it.
//
// Discovered by type assertion rather than added to Datagram: only a base
// datagram transport can answer it, and widening Datagram would make every
// other implementation carry a method it cannot honour.
type ResolvedWriter interface {
	WriteToResolved(p []byte, to Destination) (n int, sentTo Endpoint, err error)
}
