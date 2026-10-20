package http

import (
	"fmt"

	"github.com/we-apranik/apranik/module/proxy/http/wire"
	"github.com/we-apranik/apranik/spec"
)

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
