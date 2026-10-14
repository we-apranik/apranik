package registry

import "github.com/we-apranik/apranik/spec"

// APIRegistry holds the API modules: ways for another program to read and
// drive a running Apranik.
type APIRegistry struct {
	uniqueNames

	apis table[spec.APIModule]
}

func (r *APIRegistry) Register(newModule func() spec.APIModule) error {
	return register(&r.apis, newModule, KindAPI, r.claim)
}

func (r *APIRegistry) Lookup(name string) (func() spec.APIModule, error) {
	return lookup(r.apis, name, KindAPI, r.registered.kinds(name))
}
