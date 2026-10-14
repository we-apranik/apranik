package wire

import "github.com/we-apranik/apranik/spec"

// streamAfterHead is the stream a handshake hands on once the HTTP head has
// been read. It exists because some bytes must be read before the underlying
// stream again:
//
//   - the head is read through a buffer, which can take bytes past the end of
//     the head off the connection (a client may send payload right after
//     CONNECT without waiting for the reply);
//   - an absolute-form request is rewritten for the origin, and the rewritten
//     request has to reach it first.
//
// Writes, deadlines and Close go straight to the underlying stream.
type streamAfterHead struct {
	spec.Stream
	bytesToReadFirst []byte
}

func (s *streamAfterHead) Read(buffer []byte) (int, error) {
	if len(s.bytesToReadFirst) == 0 {
		return s.Stream.Read(buffer)
	}
	bytesCopied := copy(buffer, s.bytesToReadFirst)
	s.bytesToReadFirst = s.bytesToReadFirst[bytesCopied:]
	return bytesCopied, nil
}

// NewStreamAfterHead returns underlyingStream unchanged when there is nothing
// to read first.
func NewStreamAfterHead(underlyingStream spec.Stream, bytesToReadFirst []byte) spec.Stream {
	if len(bytesToReadFirst) == 0 {
		return underlyingStream
	}
	return &streamAfterHead{Stream: underlyingStream, bytesToReadFirst: bytesToReadFirst}
}
