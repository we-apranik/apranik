package httpupgrade

import (
	"bufio"
	"errors"
	"sync"
	"time"

	"github.com/we-apranik/apranik/spec"
)

type stream struct {
	spec.Stream

	handshakeReader *bufio.Reader

	pendingResponse *pendingResponse

	// Set when reading the pending 101 failed, and returned by every later
	// Read: the failed read may have consumed part of the response, so it
	// cannot be retried.
	pendingResponseErr error

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

func (s *stream) setResponseDeadline(responseDeadline time.Time) {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.responseDeadline = responseDeadline
	s.Stream.SetReadDeadline(earlierDeadline(s.callerReadDeadline, responseDeadline))
}

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
