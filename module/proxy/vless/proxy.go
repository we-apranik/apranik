// Package vless is the VLESS proxy module: a UUID, a command and a destination
// in a header, then raw payload. CONNECT gives a stream; UDP gives datagrams
// framed inside the stream, either plain (one stream carries one destination) or
// as XUDP over Mux.Cool (one stream carries every destination of a UDP session).
package vless

import (
	"fmt"
	"sync"

	"github.com/we-apranik/apranik/module/proxy/vless/wire"
	"github.com/we-apranik/apranik/shared/delayedrequest"
	"github.com/we-apranik/apranik/spec"
)

type Proxy struct {
	Settings

	xudp              bool
	muxCoolConfigured bool

	requestTiming *delayedrequest.Policy

	buildServerOnce sync.Once
	builtServer     *server
	buildServerErr  error
}

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "vless", Description: "VLESS proxy, CONNECT and UDP framed in the stream, plain or XUDP"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) { return p.newSharedServer() }

func (p *Proxy) NewDatagramOverStreamServer() (spec.DatagramOverStreamProxyServer, error) {
	return p.newSharedServer()
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) { return p.newClient() }

func (p *Proxy) NewDatagramOverStreamClient() (spec.DatagramOverStreamProxyClient, error) {
	return p.newClient()
}

func (p *Proxy) ConfigureMuxCool(options spec.MuxCoolOptions) error {
	p.xudp = options.XUDP
	p.muxCoolConfigured = true
	return nil
}

func (p *Proxy) ConfigureProxyRequestTiming(options spec.ProxyRequestTimingOptions) error {
	requestTiming, err := delayedrequest.NewPolicy(options)
	if err != nil {
		return fmt.Errorf("vless: %w", err)
	}
	p.requestTiming = &requestTiming
	return nil
}

func (p *Proxy) requestTimingPolicy() delayedrequest.Policy {
	if p.requestTiming == nil {
		return delayedrequest.DefaultPolicy()
	}
	return *p.requestTiming
}

func (p *Proxy) newSharedServer() (*server, error) {
	p.buildServerOnce.Do(func() {
		if p.buildServerErr = p.checkAsServer(); p.buildServerErr != nil {
			return
		}
		users := make([]user, len(p.Users))
		for index, configured := range p.Users {
			users[index], p.buildServerErr = newUser(configured)
			if p.buildServerErr != nil {
				return
			}
		}
		p.builtServer = newServer(users, p.handshakeTimeout(), !p.muxCoolConfigured || p.xudp)
	})
	return p.builtServer, p.buildServerErr
}

func (p *Proxy) newClient() (*client, error) {
	if err := p.checkAsClient(); err != nil {
		return nil, err
	}
	uuid, err := wire.ParseUUID(p.UUID.Reveal())
	if err != nil {
		return nil, err
	}
	return &client{uuid: uuid, requestTiming: p.requestTimingPolicy(), xudp: p.xudp}, nil
}

func NewProxyModule() spec.StreamAndDatagramOverStreamProxyModule { return &Proxy{} }
