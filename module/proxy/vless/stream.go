package vless

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/we-apranik/apranik/module/proxy/vless/wire"
	"github.com/we-apranik/apranik/spec"
)

// responseHeaderFirstStream is the server's side of an accepted stream: the
// answer header goes in front of the first thing written, in the same Write,
// so a connection never answers with a header on its own (Xray's server sends
// it with the first response data too). Closing without ever writing sends
// nothing, so a destination that could not be reached looks like a refusal;
// half-closing first sends the header, since the destination was reached and
// simply had nothing to say.
type responseHeaderFirstStream struct {
	spec.Stream

	writeMutex     sync.Mutex
	headerWritten  bool
	headerWithData []byte
}

func newResponseHeaderFirstStream(stream spec.Stream) *responseHeaderFirstStream {
	return &responseHeaderFirstStream{Stream: stream}
}

func (s *responseHeaderFirstStream) Write(payload []byte) (int, error) {
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()
	if s.headerWritten {
		return s.Stream.Write(payload)
	}
	s.headerWritten = true

	s.headerWithData = wire.AppendResponseHeader(s.headerWithData[:0])
	headerLength := len(s.headerWithData)
	written, err := s.Stream.Write(append(s.headerWithData, payload...))
	return max(written-headerLength, 0), err
}

func (s *responseHeaderFirstStream) CloseWrite() error {
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()
	if !s.headerWritten {
		s.headerWritten = true
		if _, err := s.Stream.Write(wire.AppendResponseHeader(nil)); err != nil {
			return err
		}
	}
	return s.Stream.CloseWrite()
}

// responseHeaderSkippingStream is the client's side: the server's answer header
// is read and dropped before the first payload byte is returned. Nothing is
// read until the application reads, so the client never waits for the server
// before sending.
type responseHeaderSkippingStream struct {
	spec.Stream

	readMutex  sync.Mutex
	headerRead bool
}

func (s *responseHeaderSkippingStream) Read(buffer []byte) (int, error) {
	s.readMutex.Lock()
	defer s.readMutex.Unlock()
	if !s.headerRead {
		if err := wire.ReadResponseHeader(s.Stream); err != nil {
			// A server that ends the stream without answering has nothing to
			// deliver; that is the end of the stream, not a broken header.
			if errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return 0, io.EOF
			}
			return 0, fmt.Errorf("vless: %w", err)
		}
		s.headerRead = true
	}
	return s.Stream.Read(buffer)
}
