package registry

import (
	"fmt"

	"github.com/we-apranik/apranik/spec"
)

func describe[Module module](newModule func() Module, kind Kind) (spec.ModuleInfo, error) {
	if newModule == nil {
		return spec.ModuleInfo{}, fmt.Errorf("registry: %s registered with a nil constructor", kind)
	}

	info := newModule().Info()
	if err := checkName(info.Name); err != nil {
		return spec.ModuleInfo{}, fmt.Errorf("registry: %s: %w", kind, err)
	}

	if again := newModule().Info(); again != info {
		return spec.ModuleInfo{}, &InconsistentInfoError{Kind: kind, First: info, Second: again}
	}
	return info, nil
}
