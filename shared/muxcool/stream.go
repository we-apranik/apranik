package muxcool

import (
	"net"
	"time"

	"github.com/we-apranik/apranik/spec"
)

// stream is safe for one reader and many writers.
type stream struct {
	*subConnection
}

var _ spec.Stream = (*stream)(nil)

func (s *stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	remaining, _, err := s.claimDelivery(s.readDeadline.wait())
	if err != nil {
		return 0, err
	}
	read, readErr := s.conn.reader.Read(p[:min(len(p), remaining)])
	s.finishDelivery(read, readErr)
	if readErr != nil && read == 0 {
		return 0, closedError(readErr)
	}
	return read, nil
}

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

func (s *stream) SetWriteDeadline(at time.Time) error {
	s.writeDeadline.set(at)
	return nil
}
