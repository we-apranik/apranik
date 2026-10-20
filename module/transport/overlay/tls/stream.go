package tls

import (
	cryptotls "crypto/tls"
	"errors"
	"fmt"

	utls "github.com/refraction-networking/utls"

	"github.com/we-apranik/apranik/spec"
)

type stream struct {
	*cryptotls.Conn
	underlyingStream spec.Stream
}

func (s *stream) CloseWrite() error {
	return closeWriteBoth(s.Conn.CloseWrite, s.underlyingStream)
}

type fingerprintStream struct {
	*utls.UConn
	underlyingStream spec.Stream
}

func (s *fingerprintStream) CloseWrite() error {
	return closeWriteBoth(s.UConn.CloseWrite, s.underlyingStream)
}

func closeWriteBoth(closeWriteTLS func() error, underlyingStream spec.Stream) error {
	if err := closeWriteTLS(); err != nil {
		return fmt.Errorf("tls: close write: %w", err)
	}
	if err := underlyingStream.CloseWrite(); err != nil && !errors.Is(err, spec.ErrNoHalfClose) {
		return fmt.Errorf("tls: close write below: %w", err)
	}
	return nil
}
