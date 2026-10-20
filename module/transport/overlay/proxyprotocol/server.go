package proxyprotocol

import (
	"context"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/streamdeadline"
	"github.com/we-apranik/apranik/spec"
)

type server struct {
	trusted types.AddressRanges
	timeout time.Duration
}

func newServer(settings *Settings) *server {
	return &server{trusted: settings.Verification.TrustedProxyAddresses, timeout: settings.timeout()}
}

const placementHint = "this layer must come directly after the base transport"

func (s *server) WrapServerStream(ctx context.Context, underlyingStream spec.Stream, transport *spec.TransportMetadata) (spec.Stream, error) {
	peer := transport.SourceEndpoint
	if !s.trusted.Contains(peer.Addr()) {
		return nil, fmt.Errorf("proxy_protocol: connection from %s refused: the peer is not in verification.trusted_proxy_addresses (the default is this machine only); %s", peer, placementHint)
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	release := streamdeadline.BindToContext(ctx, underlyingStream)
	parsed, err := readHeader(underlyingStream)
	if releaseErr := release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return nil, fmt.Errorf("proxy_protocol: connection from %s sent no valid PROXY header (%w); check that the proxy is configured to send it, and that the timeout of %s is enough; %s", peer, err, s.timeout, placementHint)
	}

	if parsed.hasClient {
		transport.SourceEndpoint = parsed.source
		transport.ProxyProtocol = &spec.ProxyProtocolMetadata{Version: parsed.version, SourceEndpoint: parsed.source, DestinationEndpoint: parsed.destination}
	}
	return underlyingStream, nil
}
