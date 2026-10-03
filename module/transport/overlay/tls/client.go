package tls

import (
	"context"
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/The-Apranik/Apranik/spec"
)

// Size of the resumption cache shared by every connection of one outbound.
// PROVISIONAL: Go's own default.
const sessionCacheCapacity = 64

var errVersionsNeedNoFingerprint = errors.New("tls: min_version and max_version cannot be used with fingerprint \"chrome\" (the default), whose ClientHello fixes the versions; set fingerprint to \"none\" to use them")

type client struct {
	serverName       string
	handshakeTimeout time.Duration
	// Exactly one is set: Go's own ClientHello, or a browser's through uTLS.
	goClient          *cryptotls.Config
	fingerprintClient *fingerprintClient
}

// Everything but the server name, which differs per call.
type fingerprintClient struct {
	sharedConfig *utls.Config
	helloID      utls.ClientHelloID
	// Nil keeps the preset's own list.
	applicationProtocols []string
}

func newClient(settings *Settings) (*client, error) {
	roots, err := certificateAuthorityPool(settings.CertificateAuthorities)
	if err != nil {
		return nil, err
	}
	built := &client{serverName: settings.ServerName, handshakeTimeout: settings.handshakeTimeout()}

	fingerprint := effectiveFingerprint(settings.Fingerprint)
	if fingerprint == fingerprintNone {
		built.goClient = &cryptotls.Config{
			RootCAs:            roots,
			InsecureSkipVerify: settings.Insecure,
			NextProtos:         settings.ALPN,
			MinVersion:         versionsByName[settings.MinVersion],
			MaxVersion:         versionsByName[settings.MaxVersion],
			ClientSessionCache: cryptotls.NewLRUClientSessionCache(sessionCacheCapacity),
		}
		return built, nil
	}

	// Validate catches this unless the settings also carry certificates,
	// which a client ignores.
	if settings.MinVersion != "" || settings.MaxVersion != "" {
		return nil, errVersionsNeedNoFingerprint
	}
	helloID, err := resolveClientHelloID(fingerprint)
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}
	built.fingerprintClient = &fingerprintClient{
		sharedConfig: &utls.Config{
			RootCAs:            roots,
			InsecureSkipVerify: settings.Insecure,
			ClientSessionCache: utls.NewLRUClientSessionCache(sessionCacheCapacity),
		},
		helloID:              helloID,
		applicationProtocols: settings.ALPN,
	}
	return built, nil
}

// Handshakes before returning, so a certificate problem is reported as a
// failure to reach the server rather than surfacing later inside the proxy
// protocol. No SNI is sent for an IP; the certificate's IP entries are
// checked instead.
func (c *client) WrapClientStream(ctx context.Context, underlyingStream spec.Stream, server spec.Destination) (spec.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, c.handshakeTimeout)
	defer cancel()
	serverName := c.serverName
	if serverName == "" {
		serverName = server.Host()
	}

	var wrapped spec.Stream
	var err error
	if c.goClient != nil {
		wrapped, err = c.wrapWithGoClientHello(ctx, underlyingStream, serverName)
	} else {
		wrapped, err = c.fingerprintClient.wrap(ctx, underlyingStream, serverName)
	}
	if err != nil {
		return nil, fmt.Errorf("tls: handshake with %s: %w", server, err)
	}
	return wrapped, nil
}

func (c *client) wrapWithGoClientHello(ctx context.Context, underlyingStream spec.Stream, serverName string) (spec.Stream, error) {
	config := c.goClient.Clone()
	config.ServerName = serverName
	connection := cryptotls.Client(underlyingStream, config)
	if err := connection.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return &stream{Conn: connection, underlyingStream: underlyingStream}, nil
}

func (f *fingerprintClient) wrap(ctx context.Context, underlyingStream spec.Stream, serverName string) (spec.Stream, error) {
	config := f.sharedConfig.Clone()
	config.ServerName = serverName
	connection := utls.UClient(underlyingStream, config, f.helloID)
	if err := connection.BuildHandshakeState(); err != nil {
		return nil, fmt.Errorf("building the ClientHello: %w", err)
	}
	changed := dropHybridGroupWithoutKeyShare(connection)
	if len(f.applicationProtocols) > 0 {
		replaceApplicationProtocols(connection, f.applicationProtocols)
		changed = true
	}
	if changed {
		if err := connection.BuildHandshakeState(); err != nil {
			return nil, fmt.Errorf("rebuilding the ClientHello: %w", err)
		}
	}
	if err := connection.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return &fingerprintStream{UConn: connection, underlyingStream: underlyingStream}, nil
}
