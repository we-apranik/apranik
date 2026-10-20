package http

import "github.com/we-apranik/apranik/spec"

type Proxy struct{ Settings }

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "http", Description: "HTTP proxy (CONNECT and plain http:// requests), with Basic authentication"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) {
	if err := p.checkAsServer(); err != nil {
		return nil, err
	}
	users := make([]user, len(p.Users))
	for index, configured := range p.Users {
		users[index] = newUser(configured)
	}
	server := &streamServer{handshakeTimeout: p.handshakeTimeout()}
	server.users.Store(&users)
	return server, nil
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) {
	if err := p.checkAsClient(); err != nil {
		return nil, err
	}
	return &streamClient{username: p.Username, password: p.Password.Reveal(), handshakeTimeout: p.handshakeTimeout()}, nil
}

func NewProxyModule() spec.StreamProxyModule { return &Proxy{} }
