package http

import "github.com/The-Apranik/Apranik/spec"

// Proxy fills StreamProxy only: HTTP CONNECT tunnels a stream and has no
// datagram form.
type Proxy struct{ Settings }

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "http", Description: "HTTP CONNECT proxy, with Basic authentication"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) {
	return &streamServer{username: p.Username, password: p.Password.Reveal()}, nil
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) {
	return &streamClient{username: p.Username, password: p.Password.Reveal()}, nil
}

func NewStreamProxyModule() spec.StreamProxy { return &Proxy{} }
