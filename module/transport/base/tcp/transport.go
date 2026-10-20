// Package tcp is the base stream transport: it listens for and dials plain TCP,
// which every stream stack starts from. Keep-alive and no-delay come from the
// module's settings.
package tcp

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/spec"
)

type Transport struct{ Settings }

func (*Transport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "tcp", Description: "TCP over IPv4/IPv6"}
}

var errNoResolver = errors.New("tcp: a dialer needs a resolver")

func (t *Transport) NewStreamDialer(resolver spec.Resolver) (spec.StreamDialer, error) {
	if resolver == nil {
		return nil, errNoResolver
	}
	return &streamDialer{
		dialer:           net.Dialer{KeepAlive: time.Duration(types.ValueOrDefault(t.KeepAlive, 0)), KeepAliveConfig: t.keepAliveConfig()},
		resolver:         resolver,
		noDelay:          t.NoDelay,
		dialTimeout:      t.dialTimeout(),
		nextAddressDelay: t.nextAddressDelay(),
	}, nil
}

func (t *Transport) NewStreamListener(bind spec.Endpoint) (spec.StreamListener, error) {
	address, err := bindTarget(bind)
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tcp: listen %s: %w", address, err)
	}

	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		listener.Close()
		return nil, fmt.Errorf("tcp: listen %s: got %T, want *net.TCPListener", address, listener)
	}

	return &streamListener{
		listener:  tcpListener,
		keepAlive: t.keepAliveConfig(),
		noDelay:   t.NoDelay,
	}, nil
}

func NewBaseStreamTransportModule() spec.BaseStreamTransportModule {
	return &Transport{Settings: Settings{
		KeepAlive: types.Pointer(types.Duration(15 * time.Second)),
		NoDelay:   true,
	}}
}
