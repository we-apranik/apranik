package httpupgrade

import (
	"bufio"
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

	// What the caller last set, so reading the pending 101 can put it back.
	callerReadDeadlineMutex sync.Mutex
	callerReadDeadline      time.Time
}

func (s *stream) Read(p []byte) (int, error) {
	if s.pendingResponse != nil {
		if err := s.pendingResponse.read(s); err != nil {
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
	s.callerReadDeadlineMutex.Lock()
	defer s.callerReadDeadlineMutex.Unlock()
	s.callerReadDeadline = t
	return s.Stream.SetDeadline(t)
}

func (s *stream) SetReadDeadline(t time.Time) error {
	s.callerReadDeadlineMutex.Lock()
	defer s.callerReadDeadlineMutex.Unlock()
	s.callerReadDeadline = t
	return s.Stream.SetReadDeadline(t)
}
