package registry

import (
	"fmt"

	"github.com/The-Apranik/Apranik/spec"
)

// describe is how registering any module starts: the constructor exists, the
// name is one a config file can type, and Info does not depend on a setting.
func describe[Module module](newModule func() Module, kind Kind) (spec.ModuleInfo, error) {
	if newModule == nil {
		return spec.ModuleInfo{}, fmt.Errorf("registry: %s registered with a nil constructor", kind)
	}

	info := newModule().Info()
	if err := checkName(info.Name); err != nil {
		return spec.ModuleInfo{}, fmt.Errorf("registry: %s: %w", kind, err)
	}

	// A module deriving its name from a setting would register under one name
	// and be looked up under another.
	if again := newModule().Info(); again != info {
		return spec.ModuleInfo{}, &InconsistentInfoError{Kind: kind, First: info, Second: again}
	}
	return info, nil
}
