// Package delayedrequest holds back the request a proxy client owes its server
// until the application's first payload can travel with it, so the connection
// never opens with a small packet of a fixed length on its own, which a DPI
// could recognise, and the two cost one write instead of two.
package delayedrequest

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/spec"
)

// NewStream returns stream with request put in front of the first Write, or
// sent alone once maxDelay has passed, whichever comes first. Exactly one of
func NewStream(stream spec.Stream, request []byte, maxDelay time.Duration) spec.Stream {
	s := &delayedRequestStream{Stream: stream, unsentRequest: request}
	s.requestMutex.Lock()
	defer s.requestMutex.Unlock()
	s.maxDelayTimer = time.AfterFunc(maxDelay, func() { s.sendRequestWith(nil) })
	return s
}

type delayedRequestStream struct {
	spec.Stream

	requestSent atomic.Bool

	// Guards sending the request; everything after it writes unlocked.
	requestMutex  sync.Mutex
	unsentRequest []byte
	maxDelayTimer *time.Timer
}

func (s *delayedRequestStream) Write(payload []byte) (int, error) {
	if !s.requestSent.Load() {
		sent, err := s.sendRequestWith(payload)
		if sent {
			if err != nil {
				return 0, err
			}
			return len(payload), nil
		}
	}
	return s.Stream.Write(payload)
}

func (s *delayedRequestStream) CloseWrite() error {
	if _, err := s.sendRequestWith(nil); err != nil {
		return err
	}
	return s.Stream.CloseWrite()
}

func (s *delayedRequestStream) Close() error {
	s.maxDelayTimer.Stop()
	return s.Stream.Close()
}

func (s *delayedRequestStream) sendRequestWith(payload []byte) (bool, error) {
	s.requestMutex.Lock()
	defer s.requestMutex.Unlock()
	if s.requestSent.Load() {
		return false, nil
	}
	s.maxDelayTimer.Stop()

	message := append(s.unsentRequest, payload...)
	s.unsentRequest = nil
	s.requestSent.Store(true)
	if _, err := s.Stream.Write(message); err != nil {
		return true, fmt.Errorf("delayedrequest: sending request: %w", err)
	}
	return true, nil
}
