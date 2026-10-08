package registry

import "github.com/The-Apranik/Apranik/spec"

// APIRegistry holds the API modules: ways for another program to read and
// drive a running Apranik.
type APIRegistry struct {
	apis map[string]func() spec.APIModule

	registered index
}

func (r *APIRegistry) Register(newModule func() spec.APIModule) error {
	return register(&r.apis, newModule, KindAPI, r.claim)
}

func (r *APIRegistry) Lookup(name string) (func() spec.APIModule, error) {
	return lookup(r.apis, name, KindAPI, r.registered.kinds(name))
}

func (r *APIRegistry) claim(info spec.ModuleInfo, kind Kind) error {
	if len(r.registered.kinds(info.Name)) > 0 {
		return &DuplicateModuleError{Kind: kind, Name: info.Name}
	}
	r.registered.record(info, kind)
	return nil
}

func (r *APIRegistry) Modules() []Listing { return r.registered.listings() }
