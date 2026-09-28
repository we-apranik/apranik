package registry

import "github.com/The-Apranik/Apranik/spec"

type SnifferRegistry struct {
	sniffers map[string]func() spec.SnifferModule

	registered index
}

func (r *SnifferRegistry) Register(newModule func() spec.SnifferModule) error {
	return register(&r.sniffers, newModule, KindSniffer, r.claim)
}

func (r *SnifferRegistry) Lookup(name string) (func() spec.SnifferModule, error) {
	return lookup(r.sniffers, name, KindSniffer, r.registered.kinds(name))
}

func (r *SnifferRegistry) claim(info spec.ModuleInfo, kind Kind) error {
	if len(r.registered.kinds(info.Name)) > 0 {
		return &DuplicateModuleError{Kind: kind, Name: info.Name}
	}
	r.registered.record(info, kind)
	return nil
}

func (r *SnifferRegistry) Modules() []Listing { return r.registered.listings() }
