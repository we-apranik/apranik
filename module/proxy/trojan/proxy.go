package trojan

import (
	"errors"

	"github.com/The-Apranik/Apranik/module/proxy/trojan/wire"
	"github.com/The-Apranik/Apranik/spec"
)

// Proxy fills StreamProxy for CONNECT and DatagramOverStreamProxy for UDP
// ASSOCIATE, whose datagrams are framed inside the same stream.
type Proxy struct{ Settings }

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "trojan", Description: "Trojan proxy, CONNECT and UDP framed in the stream"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) { return p.newServer() }

func (p *Proxy) NewDatagramOverStreamServer() (spec.DatagramOverStreamProxyServer, error) {
	return p.newServer()
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) { return p.newClient() }

func (p *Proxy) NewDatagramOverStreamClient() (spec.DatagramOverStreamProxyClient, error) {
	return p.newClient()
}

func (p *Proxy) newServer() (*server, error) {
	if len(p.Users) == 0 {
		return nil, errors.New("trojan: an inbound needs users")
	}
	users := make([]user, len(p.Users))
	for index, configured := range p.Users {
		users[index] = user{name: configured.Name, passwordHash: wire.HashPassword(configured.Password.Reveal())}
	}
	return &server{users: users}, nil
}

func (p *Proxy) newClient() (*client, error) {
	if p.Password == "" {
		return nil, errors.New("trojan: an outbound needs a password")
	}
	return &client{passwordHash: wire.HashPassword(p.Password.Reveal())}, nil
}

func NewStreamProxyModule() spec.StreamProxy { return &Proxy{} }

func NewDatagramOverStreamProxyModule() spec.DatagramOverStreamProxy { return &Proxy{} }
