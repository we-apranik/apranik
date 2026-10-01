package http

import (
	"bufio"
	"fmt"

	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/spec"
)

// Answers 200 and returns the underlying stream unchanged, as a tunnel; the
// payload is never read.
//
// As with SOCKS5, the 200 goes out before core has dialled, so a dial failure
// reaches the client as an EOF rather than a 502.
func acceptConnectTunnel(underlyingStream spec.Stream, headReader *bufio.Reader, metadata spec.Metadata, httpVersion, hostAndPort string) (spec.Stream, spec.Metadata, error) {
	destination, err := wire.ParseHostPort(hostAndPort, 0)
	if err != nil {
		writeStatusResponse(underlyingStream, httpVersion, statusBadRequest)
		return nil, spec.Metadata{}, err
	}
	if err := writeStatusResponse(underlyingStream, httpVersion, "200 Connection established"); err != nil {
		return nil, spec.Metadata{}, fmt.Errorf("http: sending reply: %w", err)
	}
	metadata.Destination = destination
	return wire.ReplayStream(underlyingStream, nil, headReader), metadata, nil
}
