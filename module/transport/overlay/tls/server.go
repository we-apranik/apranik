package tls

import (
	"context"
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/spec"
)

var errServerNeedsCertificate = errors.New("tls: a server needs at least one entry in certificates")

type server struct {
	config           *cryptotls.Config
	handshakeTimeout time.Duration
}

// Reads every certificate now, so a missing file or a key that does not
// match stops startup instead of failing each connection.
func newServer(settings *Settings) (*server, error) {
	if len(settings.Certificates) == 0 {
		return nil, errServerNeedsCertificate
	}
	certificates, err := loadCertificates(settings.Certificates)
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	return &server{config: &cryptotls.Config{
		Certificates: certificates,
		NextProtos:   settings.ALPN,
		MinVersion:   versionsByName[settings.MinVersion],
		MaxVersion:   versionsByName[settings.MaxVersion],
	}, handshakeTimeout: settings.handshakeTimeout()}, nil
}

func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream, _ *spec.TransportMetadata) (spec.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, s.handshakeTimeout)
	defer cancel()
	connection := cryptotls.Server(underlyingStream, s.config)
	if err := connection.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("tls: handshake with %s: %w", underlyingStream.RemoteAddr(), err)
	}
	return &stream{Conn: connection, underlyingStream: underlyingStream}, nil
}
