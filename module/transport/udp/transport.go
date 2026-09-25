// Package udp is the base datagram transport: it opens unconnected UDP sockets,
// bound to listen or created to send. It is the only layer that resolves
// domains, caching answers briefly, so every layer above keeps a domain as a
// domain.
package udp

import (
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/spec"
)

// PROVISIONAL: 5 seconds.
const DefaultResolveTimeout = 5 * time.Second

// PROVISIONAL: 30 seconds.
const DefaultResolveCacheTTL = 30 * time.Second

type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "udp", Description: "UDP over IPv4/IPv6"}
}

func (t *Transport) NewDatagramDialer() (spec.DatagramDialer, error) {
	return datagramDialer{resolver: t.resolverPolicy()}, nil
}

func (t *Transport) NewDatagramListener(bind spec.Endpoint) (spec.DatagramListener, error) {
	return newDatagramListener(bind, t.resolverPolicy())
}

func NewBaseDatagramTransportModule() spec.BaseDatagramTransport {
	return &Transport{Settings: Settings{
		ResolveTimeout:  types.Duration(DefaultResolveTimeout),
		ResolveCacheTTL: types.Duration(DefaultResolveCacheTTL),
	}}
}
