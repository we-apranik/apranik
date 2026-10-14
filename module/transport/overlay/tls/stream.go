package tls

import (
	cryptotls "crypto/tls"
	"errors"
	"fmt"

	utls "github.com/refraction-networking/utls"

	"github.com/we-apranik/apranik/spec"
)

// stream is a crypto/tls connection as a spec.Stream: a server's, or a client
// with fingerprint "none". crypto/tls already passes deadlines and addresses
// through to the stream below; only CloseWrite needs more than it does.
type stream struct {
	*cryptotls.Conn
	underlyingStream spec.Stream
}

func (s *stream) CloseWrite() error {
	return closeWriteBoth(s.Conn.CloseWrite, s.underlyingStream)
}

// fingerprintStream is a uTLS client connection as a spec.Stream.
type fingerprintStream struct {
	*utls.UConn
	underlyingStream spec.Stream
}

func (s *fingerprintStream) CloseWrite() error {
	return closeWriteBoth(s.UConn.CloseWrite, s.underlyingStream)
}

// TLS's CloseWrite sends close_notify, which the peer reads as end of stream,
// but leaves the stream below open for writing. Half-closing that too lets a
// layer beneath see the end as well. A stream below that cannot half-close is
// no failure: close_notify already said it.
func closeWriteBoth(closeWriteTLS func() error, underlyingStream spec.Stream) error {
	if err := closeWriteTLS(); err != nil {
		return fmt.Errorf("tls: close write: %w", err)
	}
	if err := underlyingStream.CloseWrite(); err != nil && !errors.Is(err, spec.ErrNoHalfClose) {
		return fmt.Errorf("tls: close write below: %w", err)
	}
	return nil
}
