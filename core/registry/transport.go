package registry

import "github.com/The-Apranik/Apranik/spec"

// TransportRegistry is the transport namespace, independent of the proxy one.
type TransportRegistry struct {
	baseStream      map[string]func() spec.BaseStreamTransport
	overlayStream   map[string]func() spec.OverlayStreamTransport
	baseDatagram    map[string]func() spec.BaseDatagramTransport
	overlayDatagram map[string]func() spec.OverlayDatagramTransport

	registered index
}

func (r *TransportRegistry) RegisterBaseStream(newModule func() spec.BaseStreamTransport) error {
	return register(&r.baseStream, newModule, KindBaseStream, r.claim)
}

func (r *TransportRegistry) RegisterOverlayStream(newModule func() spec.OverlayStreamTransport) error {
	return register(&r.overlayStream, newModule, KindOverlayStream, r.claim)
}

func (r *TransportRegistry) RegisterBaseDatagram(newModule func() spec.BaseDatagramTransport) error {
	return register(&r.baseDatagram, newModule, KindBaseDatagram, r.claim)
}

func (r *TransportRegistry) RegisterOverlayDatagram(newModule func() spec.OverlayDatagramTransport) error {
	return register(&r.overlayDatagram, newModule, KindOverlayDatagram, r.claim)
}

func (r *TransportRegistry) LookupBaseStream(name string) (func() spec.BaseStreamTransport, error) {
	return lookup(r.baseStream, name, KindBaseStream, r.registered.kinds(name))
}

func (r *TransportRegistry) LookupOverlayStream(name string) (func() spec.OverlayStreamTransport, error) {
	return lookup(r.overlayStream, name, KindOverlayStream, r.registered.kinds(name))
}

func (r *TransportRegistry) LookupBaseDatagram(name string) (func() spec.BaseDatagramTransport, error) {
	return lookup(r.baseDatagram, name, KindBaseDatagram, r.registered.kinds(name))
}

func (r *TransportRegistry) LookupOverlayDatagram(name string) (func() spec.OverlayDatagramTransport, error) {
	return lookup(r.overlayDatagram, name, KindOverlayDatagram, r.registered.kinds(name))
}

// claim holds the whole keyspace rule, so the four Register methods stay
// identical in shape.
func (r *TransportRegistry) claim(info spec.ModuleInfo, kind Kind) error {
	for _, existing := range r.registered.kinds(info.Name) {
		if existing == kind {
			return &DuplicateModuleError{Kind: kind, Name: info.Name}
		}
		if existing.carrier() == kind.carrier() {
			return &KindConflictError{Name: info.Name, Existing: existing, Wanted: kind}
		}
	}
	r.registered.record(info, kind)
	return nil
}

func (r *TransportRegistry) Modules() []Listing { return r.registered.listings() }
