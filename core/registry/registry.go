// Package registry maps the names a config file uses, such as "tcp" or
// "socks5", to the modules that implement them. Registration is explicit, with
// no init() and no globals, so an embedder can build a registry that includes
// its own modules.
package registry

// A value, not a global: no init(), no blank imports. The zero value is
// usable. Not safe for concurrent registration.
type Registry struct {
	transports     TransportRegistry
	proxyProtocols ProxyProtocolRegistry
	sniffers       SnifferRegistry
	dnsServers     DNSServerRegistry
	apis           APIRegistry
}

func New() *Registry { return &Registry{} }

func (r *Registry) Transports() *TransportRegistry { return &r.transports }

func (r *Registry) ProxyProtocols() *ProxyProtocolRegistry {
	return &r.proxyProtocols
}

func (r *Registry) Sniffers() *SnifferRegistry { return &r.sniffers }

func (r *Registry) DNSServers() *DNSServerRegistry { return &r.dnsServers }

func (r *Registry) APIs() *APIRegistry { return &r.apis }
