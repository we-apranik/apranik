package tls

import (
	cryptotls "crypto/tls"
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/spec"
)

// stream is the TLS connection as a spec.Stream. crypto/tls already passes
// deadlines and addresses through to the stream below; only CloseWrite needs
// more than it does.
type stream struct {
	*cryptotls.Conn
	underlyingStream spec.Stream
}

// crypto/tls's CloseWrite sends close_notify, which the peer reads as end of
// stream, but leaves the stream below open for writing. Half-closing that
// too lets a layer beneath see the end as well. A stream below that cannot
// half-close is no failure: close_notify already said it.
func (s *stream) CloseWrite() error {
	if err := s.Conn.CloseWrite(); err != nil {
		return fmt.Errorf("tls: close write: %w", err)
	}
	if err := s.underlyingStream.CloseWrite(); err != nil && !errors.Is(err, spec.ErrNoHalfClose) {
		return fmt.Errorf("tls: close write below: %w", err)
	}
	return nil
}
