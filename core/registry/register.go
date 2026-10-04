package registry

import (
	"fmt"

	"github.com/The-Apranik/Apranik/spec"
)

// Info is all ten kind interfaces have in common. The tables stay typed;
// this only lets one register function serve them all.
type module interface {
	Info() spec.ModuleInfo
}

// Stores a constructor, not a module: its return type is the kind, so a module
// registered under the wrong one fails to compile at the call site.
func register[Module module](
	table *map[string]func() Module,
	newModule func() Module,
	kind Kind,
	claim func(info spec.ModuleInfo, kind Kind) error,
) error {
	if newModule == nil {
		return fmt.Errorf("registry: %s registered with a nil constructor", kind)
	}

	info := newModule().Info()
	if err := checkName(info.Name); err != nil {
		return fmt.Errorf("registry: %s: %w", kind, err)
	}

	// A module deriving its name from a setting would register under one name
	// and be looked up under another.
	if again := newModule().Info(); again != info {
		return &InconsistentInfoError{Kind: kind, First: info, Second: again}
	}

	if err := claim(info, kind); err != nil {
		return err
	}

	if *table == nil {
		*table = make(map[string]func() Module)
	}
	(*table)[info.Name] = newModule
	return nil
}

func lookup[Module module](
	table map[string]func() Module,
	name string,
	kind Kind,
	registered []Kind,
) (func() Module, error) {
	newModule, found := table[name]
	if !found {
		return nil, &UnknownModuleError{Name: name, Want: kind, Registered: registered}
	}
	return newModule, nil
}

// A key appears verbatim in a config file, so it stays to what a user can type
// without quoting surprises.
func checkName(name string) error {
	if name == "" {
		return errEmptyName
	}
	if !isLower(name[0]) {
		return fmt.Errorf("name %q must start with a lowercase letter", name)
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if !isLower(character) && !isDigit(character) && character != '-' && character != '_' {
			return fmt.Errorf("name %q may use only lowercase letters, digits, - and _", name)
		}
	}
	return nil
}

func isLower(character byte) bool { return character >= 'a' && character <= 'z' }
func isDigit(character byte) bool { return character >= '0' && character <= '9' }
