// Package udp is the base datagram transport: it opens unconnected UDP sockets,
// bound to listen or created to send. It is the only layer that resolves
// domains, through the resolver it is given, so every layer above keeps a
// domain as a domain.
package udp

import (
	"errors"

	"github.com/The-Apranik/Apranik/spec"
)

type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "udp", Description: "UDP over IPv4/IPv6"}
}

var errNoResolver = errors.New("udp: a socket needs a resolver")

func (t *Transport) NewDatagramDialer(resolver spec.Resolver) (spec.DatagramDialer, error) {
	if resolver == nil {
		return nil, errNoResolver
	}
	return datagramDialer{resolver: resolver}, nil
}

func (t *Transport) NewDatagramListener(bind spec.Endpoint, resolver spec.Resolver) (spec.DatagramListener, error) {
	if resolver == nil {
		return nil, errNoResolver
	}
	return newDatagramListener(bind, resolver)
}

func NewBaseDatagramTransportModule() spec.BaseDatagramTransportModule {
	return &Transport{}
}
