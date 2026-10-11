// Package vless is the VLESS proxy module: a UUID, a command and a destination
// in a header, then raw payload. CONNECT gives a stream; UDP gives datagrams
// framed inside the stream, plain (no XUDP): one stream carries one destination.
package vless

import (
	"sync"

	"github.com/The-Apranik/Apranik/module/proxy/vless/wire"
	"github.com/The-Apranik/Apranik/spec"
)

// Proxy fills StreamProxyModule for CONNECT and DatagramOverStreamProxyModule
// for UDP, whose datagrams are framed inside the same stream. It is registered
// once, so one Proxy is decoded for an inbound and its server is built once and
// serves both: reusing a Proxy for a second inbound would give that inbound the
// same server.
type Proxy struct {
	Settings

	buildServerOnce sync.Once
	builtServer     *server
	buildServerErr  error
}

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "vless", Description: "VLESS proxy, CONNECT and plain UDP framed in the stream"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) { return p.newSharedServer() }

func (p *Proxy) NewDatagramOverStreamServer() (spec.DatagramOverStreamProxyServer, error) {
	return p.newSharedServer()
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) { return p.newClient() }

func (p *Proxy) NewDatagramOverStreamClient() (spec.DatagramOverStreamProxyClient, error) {
	return p.newClient()
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
		p.builtServer = newServer(users, p.handshakeTimeout())
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
	return &client{uuid: uuid, maxFirstPayloadDelay: p.maxFirstPayloadDelay()}, nil
}

func NewProxyModule() spec.StreamAndDatagramOverStreamProxyModule { return &Proxy{} }
