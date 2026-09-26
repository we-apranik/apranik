// Package peekable is core's spec.PeekableStream: a stream whose next bytes
// can be looked at before they are read. A recording stream also keeps every
// byte a handshake reads, so a request a protocol refuses can be replayed, from
// its first byte, to a fallback.
package peekable

import (
	"errors"

	"github.com/The-Apranik/Apranik/spec"
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

	// Bytes taken from the inner stream and not yet dropped. With recording
	// on, this starts at the first byte ever read.
	buffered []byte

	// How much of buffered Read has already handed out.
	readOffset int

	recording  bool
	overflowed bool
}

var _ spec.PeekableStream = (*Stream)(nil)

func NewStream(inner spec.Stream) *Stream { return &Stream{Stream: inner} }

// NewRecordingStream keeps everything read until StopRecording, so Rewind can
// replay it.
func NewRecordingStream(inner spec.Stream) *Stream {
	return &Stream{Stream: inner, recording: true}
}

func (s *Stream) Peek(n int) ([]byte, error) {
	if !s.recording {
		s.dropReadBytes()
	}
	if s.readOffset+n > MaxBufferedBytes {
		return nil, ErrBufferFull
	}

	var err error
	for len(s.buffered)-s.readOffset < n && err == nil {
		err = s.fill(s.readOffset + n)
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

// Reads at least one byte more, never past the buffer's limit.
func (s *Stream) fill(wanted int) error {
	room := max(wanted, min(MaxBufferedBytes, 2*wanted)) - len(s.buffered)
	chunk := make([]byte, room)
	size, err := s.Stream.Read(chunk)
	s.buffered = append(s.buffered, chunk[:size]...)
	return err
}
