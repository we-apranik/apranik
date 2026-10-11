// Package delayedrequest holds back the request a proxy client owes its server
// until the application's first payload can travel with it, so the connection
// never opens with a small packet of a fixed length on its own, which a DPI
// could recognise, and the two cost one write instead of two.
//
// A server-first protocol (SSH, SMTP) sends nothing until the server has
// spoken, and the server speaks only after the request, so the request cannot
// be held for ever: once the maximum delay runs out it goes alone.
package delayedrequest

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// NewStream returns stream with request put in front of the first Write, or
// sent alone once maxDelay has passed, whichever comes first. Exactly one of
// them sends it. A CloseWrite before any payload also owes the server its
// request, so it sends it first.
//
// The request is not copied; the caller must not change it afterwards.
func NewStream(stream spec.Stream, request []byte, maxDelay time.Duration) spec.Stream {
	s := &delayedRequestStream{Stream: stream, unsentRequest: request}
	// Held while the timer is stored, so a timer firing at once finds it set.
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

// Reports whether this call sent the request, payload included.
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
