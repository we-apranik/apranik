package trojan

import (
	"fmt"
	"sync"

	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/shared/delayedrequest"
	"github.com/we-apranik/apranik/spec"
)

type Proxy struct {
	Settings

	requestTiming *delayedrequest.Policy

	buildServerOnce sync.Once
	builtServer     *server
	buildServerErr  error
}

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "trojan", Description: "Trojan proxy, CONNECT and UDP framed in the stream"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) { return p.newSharedServer() }

func (p *Proxy) NewDatagramOverStreamServer() (spec.DatagramOverStreamProxyServer, error) {
	return p.newSharedServer()
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) { return p.newClient() }

func (p *Proxy) NewDatagramOverStreamClient() (spec.DatagramOverStreamProxyClient, error) {
	return p.newClient()
}

func (p *Proxy) ConfigureProxyRequestTiming(options spec.ProxyRequestTimingOptions) error {
	requestTiming, err := delayedrequest.NewPolicy(options)
	if err != nil {
		return fmt.Errorf("trojan: %w", err)
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
			users[index] = newUser(configured)
		}
		p.builtServer = newServer(users, p.handshakeTimeout())
	})
	return p.builtServer, p.buildServerErr
}

func (p *Proxy) newClient() (*client, error) {
	if err := p.checkAsClient(); err != nil {
		return nil, err
	}
	return &client{passwordHash: wire.HashPassword(p.Password.Reveal()), requestTiming: p.requestTimingPolicy()}, nil
}

func NewProxyModule() spec.StreamAndDatagramOverStreamProxyModule { return &Proxy{} }
