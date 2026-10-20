package registry

import "github.com/we-apranik/apranik/spec"

type uniqueNames struct{ registered index }

func (u *uniqueNames) claim(info spec.ModuleInfo, kind Kind) error {
	if len(u.registered.kinds(info.Name)) > 0 {
		return &DuplicateModuleError{Kind: kind, Name: info.Name}
	}
	u.registered.record(info, kind)
	return nil
}

func (u *uniqueNames) Modules() []Listing { return u.registered.listings() }
