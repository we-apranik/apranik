package wire

import "github.com/we-apranik/apranik/spec"

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

func NewStreamAfterHead(underlyingStream spec.Stream, bytesToReadFirst []byte) spec.Stream {
	if len(bytesToReadFirst) == 0 {
		return underlyingStream
	}
	return &streamAfterHead{Stream: underlyingStream, bytesToReadFirst: bytesToReadFirst}
}
