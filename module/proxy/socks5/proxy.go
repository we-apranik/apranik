package socks5

import "github.com/The-Apranik/Apranik/spec"

// Proxy fills StreamProxyModule and nothing else. UDP ASSOCIATE is served
// through the stream kind rather than through DatagramProxyModule: spec's
// datagram kinds are handed a datagram, and this protocol has to negotiate on
// TCP before a datagram socket exists.
type Proxy struct{ Settings }

func (*Proxy) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "socks5", Description: "SOCKS5 proxy (RFC 1928), with UDP ASSOCIATE"}
}

func (p *Proxy) NewStreamServer() (spec.StreamProxyServer, error) {
	return &streamServer{
		username:         p.Username,
		password:         p.Password.Reveal(),
		udpRelay:         p.UDPRelay,
		handshakeTimeout: p.handshakeTimeout(),
		maxDatagramSize:  p.maxDatagramSize(),
	}, nil
}

func (p *Proxy) NewStreamClient() (spec.StreamProxyClient, error) {
	return &streamClient{
		username:         p.Username,
		password:         p.Password.Reveal(),
		handshakeTimeout: p.handshakeTimeout(),
	}, nil
}

func NewStreamProxyModule() spec.StreamProxyModule {
	return &Proxy{Settings: Settings{UDPRelay: true}}
}
