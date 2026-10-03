package http

import "github.com/The-Apranik/Apranik/spec"

// Proxy fills StreamProxyModule only: HTTP proxying carries TCP and has no
// datagram form.
type Proxy struct{ Settings }

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "http", Description: "HTTP proxy (CONNECT and plain http:// requests), with Basic authentication"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) {
	return &streamServer{username: p.Username, password: p.Password.Reveal(), handshakeTimeout: p.handshakeTimeout()}, nil
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) {
	return &streamClient{username: p.Username, password: p.Password.Reveal(), handshakeTimeout: p.handshakeTimeout()}, nil
}

func NewStreamProxyModule() spec.StreamProxyModule { return &Proxy{} }
