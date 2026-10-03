package httpupgrade

import (
	"bufio"
	"errors"
	"sync"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// stream is the stream below, except that bytes the handshake read past its
// end come first. Once they are gone it reads the stream below directly, so
// there is no copy and nothing to pay after the handshake.
type stream struct {
	spec.Stream

	// Nil once drained.
	handshakeReader *bufio.Reader

	// Client with pipelining: the 101 is read on the first Read. Nil once read.
	pendingResponse *pendingResponse

	// Set when reading the pending 101 failed, and returned by every later
	// Read: the failed read may have consumed part of the response, so it
	// cannot be retried.
	pendingResponseErr error

	// The stream below gets the earlier of the caller's read deadline and,
	// while the pending 101 is read, the response deadline, so a deadline the
	// caller sets during that wait applies at once and the handshake's bound
	// still holds.
	deadlinesMutex     sync.Mutex
	callerReadDeadline time.Time
	responseDeadline   time.Time
}

func (s *stream) Read(p []byte) (int, error) {
	if s.pendingResponseErr != nil {
		return 0, s.pendingResponseErr
	}
	if s.pendingResponse != nil {
		if err := s.pendingResponse.read(s); err != nil {
			s.pendingResponseErr = err
			return 0, err
		}
		s.pendingResponse = nil
	}
	if s.handshakeReader != nil {
		if s.handshakeReader.Buffered() > 0 {
			return s.handshakeReader.Read(p)
		}
		s.handshakeReader = nil
	}
	return s.Stream.Read(p)
}

func (s *stream) SetDeadline(t time.Time) error {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.callerReadDeadline = t
	readErr := s.Stream.SetReadDeadline(earlierDeadline(t, s.responseDeadline))
	writeErr := s.Stream.SetWriteDeadline(t)
	return errors.Join(readErr, writeErr)
}

func (s *stream) SetReadDeadline(t time.Time) error {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.callerReadDeadline = t
	return s.Stream.SetReadDeadline(earlierDeadline(t, s.responseDeadline))
}

// The zero time ends the response's bound; the caller's read deadline stays.
func (s *stream) setResponseDeadline(responseDeadline time.Time) {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.responseDeadline = responseDeadline
	s.Stream.SetReadDeadline(earlierDeadline(s.callerReadDeadline, responseDeadline))
}

// The zero time means no deadline, so it is later than any other.
func earlierDeadline(first, second time.Time) time.Time {
	switch {
	case first.IsZero():
		return second
	case second.IsZero() || first.Before(second):
		return first
	default:
		return second
	}
}
