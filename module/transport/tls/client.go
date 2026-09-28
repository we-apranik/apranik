package tls

import (
	"context"
	cryptotls "crypto/tls"
	"fmt"

	"github.com/The-Apranik/Apranik/spec"
)

// Size of the resumption cache shared by every connection of one outbound.
// PROVISIONAL: Go's own default.
const sessionCacheCapacity = 64

type client struct {
	// Everything but the server name, which differs per call.
	sharedConfig *cryptotls.Config
	serverName   string
}

func newClient(settings *Settings) (*client, error) {
	roots, err := certificateAuthorityPool(settings.CertificateAuthorities)
	if err != nil {
		return nil, err
	}
	return &client{
		sharedConfig: &cryptotls.Config{
			RootCAs:            roots,
			InsecureSkipVerify: settings.Insecure,
			NextProtos:         settings.ALPN,
			MinVersion:         versionsByName[settings.MinVersion],
			MaxVersion:         versionsByName[settings.MaxVersion],
			ClientSessionCache: cryptotls.NewLRUClientSessionCache(sessionCacheCapacity),
		},
		serverName: settings.ServerName,
	}, nil
}

// Handshakes before returning, so a certificate problem is reported as a
// failure to reach the server rather than surfacing later inside the proxy
// protocol. crypto/tls sends no SNI for an IP and checks the certificate's IP
// entries instead.
func (c *client) WrapClientStream(ctx context.Context, underlyingStream spec.Stream, server spec.Destination) (spec.Stream, error) {
	config := c.sharedConfig.Clone()
	config.ServerName = c.serverName
	if config.ServerName == "" {
		config.ServerName = server.Host()
	}

	connection := cryptotls.Client(underlyingStream, config)
	if err := connection.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("tls: handshake with %s: %w", server, err)
	}
	return &stream{Conn: connection, underlyingStream: underlyingStream}, nil
}
