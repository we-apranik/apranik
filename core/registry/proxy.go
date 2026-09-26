package registry

import "github.com/The-Apranik/Apranik/spec"

// ProxyProtocolRegistry is the proxy namespace: a transport and a proxy
// protocol may share a name without colliding. A module filling more than one
// kind registers once per kind, and any combination is allowed -- unlike
// transports, nothing about position makes one ambiguous.
type ProxyProtocolRegistry struct {
	stream             map[string]func() spec.StreamProxy
	datagramOverStream map[string]func() spec.DatagramOverStreamProxy
	datagram           map[string]func() spec.DatagramProxy

	registered index
}

func (r *ProxyProtocolRegistry) RegisterStream(newModule func() spec.StreamProxy) error {
	return register(&r.stream, newModule, KindStreamProxy, r.claim)
}

func (r *ProxyProtocolRegistry) RegisterDatagramOverStream(newModule func() spec.DatagramOverStreamProxy) error {
	return register(&r.datagramOverStream, newModule, KindDatagramOverStreamProxy, r.claim)
}

func (r *ProxyProtocolRegistry) RegisterDatagram(newModule func() spec.DatagramProxy) error {
	return register(&r.datagram, newModule, KindDatagramProxy, r.claim)
}

func (r *ProxyProtocolRegistry) LookupStream(name string) (func() spec.StreamProxy, error) {
	return lookup(r.stream, name, KindStreamProxy, r.registered.kinds(name))
}

func (r *ProxyProtocolRegistry) LookupDatagramOverStream(name string) (func() spec.DatagramOverStreamProxy, error) {
	return lookup(r.datagramOverStream, name, KindDatagramOverStreamProxy, r.registered.kinds(name))
}

func (r *ProxyProtocolRegistry) LookupDatagram(name string) (func() spec.DatagramProxy, error) {
	return lookup(r.datagram, name, KindDatagramProxy, r.registered.kinds(name))
}

func (r *ProxyProtocolRegistry) claim(info spec.ModuleInfo, kind Kind) error {
	if info.Name == FreedomProtocolName || info.Name == BlockProtocolName || info.Name == MixedProtocolName {
		return &ReservedNameError{Kind: kind, Name: info.Name}
	}
	for _, existing := range r.registered.kinds(info.Name) {
		if existing == kind {
			return &DuplicateModuleError{Kind: kind, Name: info.Name}
		}
	}
	r.registered.record(info, kind)
	return nil
}

func (r *ProxyProtocolRegistry) Modules() []Listing { return r.registered.listings() }
