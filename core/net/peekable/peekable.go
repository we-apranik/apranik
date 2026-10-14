// Package peekable is core's spec.PeekableStream: a stream whose next bytes
// can be looked at before they are read. A recording stream also keeps every
// byte a handshake reads, so a request a protocol refuses can be replayed, from
// its first byte, to a fallback.
package peekable

import (
	"errors"

	"github.com/we-apranik/apranik/spec"
)

// Large enough for a post-quantum TLS ClientHello split across records, and
// for any proxy request header.
const MaxBufferedBytes = 16 << 10

var (
	ErrBufferFull = errors.New("peekable: more bytes asked for than the buffer holds")

	// ErrNotRewindable means the handshake read more than MaxBufferedBytes, or
	// recording had already stopped, so the bytes to replay are gone.
	ErrNotRewindable = errors.New("peekable: the bytes read so far were not kept")
)

// Stream is safe for one reader and any number of writers, which is what a
// handshake followed by a relay needs.
type Stream struct {
	spec.Stream

	// Bytes taken from the underlying stream and not yet dropped. With recording
	// on, this starts at the first byte ever read.
	buffered []byte

	// How much of buffered Read has already handed out.
	readOffset int

	recording  bool
	overflowed bool
}

var _ spec.PeekableStream = (*Stream)(nil)

func NewStream(underlyingStream spec.Stream) *Stream { return &Stream{Stream: underlyingStream} }

// NewRecordingStream keeps everything read until StopRecording, so Rewind can
// replay it.
func NewRecordingStream(underlyingStream spec.Stream) *Stream {
	return &Stream{Stream: underlyingStream, recording: true}
}

func (s *Stream) Peek(n int) ([]byte, error) { return s.peek(n, true) }

func (s *Stream) PeekExactly(n int) ([]byte, error) { return s.peek(n, false) }

func (s *Stream) peek(n int, readAhead bool) ([]byte, error) {
	if !s.recording {
		s.dropReadBytes()
	}
	if s.readOffset+n > MaxBufferedBytes {
		return nil, ErrBufferFull
	}

	var err error
	for len(s.buffered)-s.readOffset < n && err == nil {
		err = s.fill(s.readOffset+n, readAhead)
	}

	available := s.buffered[s.readOffset:]
	if len(available) >= n {
		return available[:n], nil
	}
	return available, err
}

// Buffered reports how many bytes Peek can return without reading.
func (s *Stream) Buffered() int { return len(s.buffered) - s.readOffset }

func (s *Stream) Read(destination []byte) (int, error) {
	if s.readOffset < len(s.buffered) {
		copied := copy(destination, s.buffered[s.readOffset:])
		s.readOffset += copied
		if !s.recording {
			s.dropReadBytes()
		}
		return copied, nil
	}

	if !s.recording || s.overflowed {
		return s.Stream.Read(destination)
	}

	size, err := s.Stream.Read(destination)
	if len(s.buffered)+size > MaxBufferedBytes {
		s.overflowed = true
		s.buffered, s.readOffset = nil, 0
		return size, err
	}
	s.buffered = append(s.buffered, destination[:size]...)
	s.readOffset = len(s.buffered)
	return size, err
}

// Rewind makes the next Read start again at the first byte recorded.
func (s *Stream) Rewind() error {
	if !s.recording || s.overflowed {
		return ErrNotRewindable
	}
	s.readOffset = 0
	return nil
}

// StopRecording drops the bytes already read; bytes peeked but not yet read
// are still delivered.
func (s *Stream) StopRecording() {
	s.recording = false
	s.dropReadBytes()
}

func (s *Stream) dropReadBytes() {
	s.buffered = s.buffered[s.readOffset:]
	s.readOffset = 0
	if len(s.buffered) == 0 {
		s.buffered = nil
	}
}

// A read returns what has already arrived without waiting to fill the room, so
// offering this much lets a whole first message, such as a TLS ClientHello,
// land in one read instead of a round per doubling.
const readAheadFloor = 4 << 10

// Reads at least one byte more, straight into the buffer's spare capacity. With
// read-ahead it offers room for at least readAheadFloor or twice wanted, never
// past the buffer's limit; without it, never past wanted.
func (s *Stream) fill(wanted int, readAhead bool) error {
	target := wanted
	if readAhead {
		target = max(wanted, min(MaxBufferedBytes, max(2*wanted, readAheadFloor)))
	}
	if cap(s.buffered) < target {
		grown := make([]byte, len(s.buffered), target)
		copy(grown, s.buffered)
		s.buffered = grown
	}
	size, err := s.Stream.Read(s.buffered[len(s.buffered):target])
	s.buffered = s.buffered[:len(s.buffered)+size]
	return err
}
