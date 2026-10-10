package registry

import "github.com/The-Apranik/Apranik/spec"

type SnifferRegistry struct {
	uniqueNames

	sniffers table[spec.SnifferModule]
}

func (r *SnifferRegistry) Register(newModule func() spec.SnifferModule) error {
	return register(&r.sniffers, newModule, KindSniffer, r.claim)
}

func (r *SnifferRegistry) Lookup(name string) (func() spec.SnifferModule, error) {
	return lookup(r.sniffers, name, KindSniffer, r.registered.kinds(name))
}
