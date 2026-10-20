package registry

import "github.com/we-apranik/apranik/spec"

const SystemDNSServerName = "system"

type DNSServerRegistry struct {
	uniqueNames

	stream   table[spec.StreamDNSServerModule]
	datagram table[spec.DatagramDNSServerModule]
}

func (r *DNSServerRegistry) RegisterStream(newModule func() spec.StreamDNSServerModule) error {
	return register(&r.stream, newModule, KindStreamDNSServer, r.claim)
}

func (r *DNSServerRegistry) RegisterDatagram(newModule func() spec.DatagramDNSServerModule) error {
	return register(&r.datagram, newModule, KindDatagramDNSServer, r.claim)
}

func (r *DNSServerRegistry) LookupStream(name string) (func() spec.StreamDNSServerModule, error) {
	return lookup(r.stream, name, KindStreamDNSServer, r.registered.kinds(name))
}

func (r *DNSServerRegistry) LookupDatagram(name string) (func() spec.DatagramDNSServerModule, error) {
	return lookup(r.datagram, name, KindDatagramDNSServer, r.registered.kinds(name))
}

func (r *DNSServerRegistry) Kinds(name string) []Kind { return r.registered.kinds(name) }

func (r *DNSServerRegistry) claim(info spec.ModuleInfo, kind Kind) error {
	if info.Name == SystemDNSServerName {
		return &ReservedNameError{Kind: kind, Name: info.Name}
	}
	return r.uniqueNames.claim(info, kind)
}
