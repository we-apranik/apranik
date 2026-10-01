package http

import (
	"fmt"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/spec"
)

// Answers 200 and returns the stream as a tunnel; the payload is never read.
//
// As with SOCKS5, the 200 goes out before core has dialled, so a dial failure
// reaches the client as an EOF rather than a 502.
func acceptConnectTunnel(streamAfterHead spec.Stream, metadata spec.Metadata, httpVersion, hostAndPort string) (spec.Stream, spec.Metadata, error) {
	destination, err := wire.ParseHostPort(hostAndPort, 0)
	if err != nil {
		writeStatusResponse(streamAfterHead, httpVersion, statusBadRequest)
		return nil, spec.Metadata{}, err
	}
	if err := writeStatusResponse(streamAfterHead, httpVersion, statusConnectionEstablished); err != nil {
		return nil, spec.Metadata{}, fmt.Errorf("http: sending reply: %w", err)
	}
	metadata.Destination = destination
	return streamAfterHead, metadata, nil
}
