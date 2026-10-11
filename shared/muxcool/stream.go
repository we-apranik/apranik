package muxcool

import (
	"net"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// stream is a TCP sub-connection. It is safe for one reader and many writers.
type stream struct {
	*subConnection
}

var _ spec.Stream = (*stream)(nil)

func (s *stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	remaining, _, err := s.awaitDelivery(s.readDeadline.wait())
	if err != nil {
		return 0, err
	}
	read, readErr := s.conn.reader.Read(p[:min(len(p), remaining)])
	s.finishRead(read, readErr)
	if readErr != nil && read == 0 {
		return 0, closedError(readErr)
	}
	return read, nil
}

// Write cuts p into frames of at most 8 KiB, so no sub-connection holds the
// carrier for long and the others interleave.
func (s *stream) Write(p []byte) (int, error) {
	if err := s.writable(); err != nil {
		return 0, err
	}
	written := 0
	for written < len(p) {
		chunk := p[written:min(len(p), written+maxStreamFrameData)]
		if err := s.writeKeep(chunk); err != nil {
			return written, err
		}
		written += len(chunk)
	}
	return written, nil
}

func (s *stream) Close() error { return s.close() }

// CloseWrite is not possible: End ends a sub-connection in both directions.
func (s *stream) CloseWrite() error { return spec.ErrNoHalfClose }

func (s *stream) LocalAddr() net.Addr  { return subConnectionAddress(s.id) }
func (s *stream) RemoteAddr() net.Addr { return destinationAddress(NetworkTCP, s.destination.String()) }

func (s *stream) SetDeadline(at time.Time) error {
	s.readDeadline.set(at)
	s.writeDeadline.set(at)
	return nil
}

func (s *stream) SetReadDeadline(at time.Time) error {
	s.readDeadline.set(at)
	return nil
}

// A write deadline bounds the wait for the carrier's turn. A frame whose write
// has begun is not interrupted.
func (s *stream) SetWriteDeadline(at time.Time) error {
	s.writeDeadline.set(at)
	return nil
}
