package registry

import (
	"errors"
	"fmt"
	"strings"

	"github.com/The-Apranik/Apranik/spec"
)

var errEmptyName = errors.New("name is empty")

type DuplicateModuleError struct {
	Kind Kind
	Name string
}

func (e *DuplicateModuleError) Error() string {
	return fmt.Sprintf("registry: %s %q is already registered", e.Kind, e.Name)
}

// Special protocols built by core; a proxy module may not take their names.
// block is no longer an outbound but stays reserved, so a config written for
// the old block outbound is told where blocking went, not handed to a module.
const (
	FreedomProtocolName = "freedom"
	BlockProtocolName   = "block"
	MixedProtocolName   = "mixed"

	// The outbound groups: protocols that choose among other outbounds.
	URLTestProtocolName  = "urltest"
	FallbackProtocolName = "fallback"
	SelectorProtocolName = "selector"
)

type ReservedNameError struct {
	Kind Kind
	Name string
}

func (e *ReservedNameError) Error() string {
	return fmt.Sprintf("registry: %s name %q is reserved for the special protocol built by core", e.Kind, e.Name)
}

// KindConflictError is the transport keyspace rule: one name may be a base and
// an overlay only when their carrier shapes differ.
type KindConflictError struct {
	Name     string
	Existing Kind
	Wanted   Kind
}

func (e *KindConflictError) Error() string {
	return fmt.Sprintf("registry: %q is already a %s, so it cannot also be a %s: a name may hold two transport kinds only when their carrier shapes differ",
		e.Name, e.Existing, e.Wanted)
}

type InconsistentInfoError struct {
	Kind          Kind
	First, Second spec.ModuleInfo
}

func (e *InconsistentInfoError) Error() string {
	return fmt.Sprintf("registry: %s Info() is not constant: got %q then %q; Info identifies the module, so it must not depend on any setting",
		e.Kind, e.First.Name, e.Second.Name)
}

// UnknownModuleError carries the kinds the name really holds, so a caller can
// tell "no such module" apart from "right module, wrong position".
type UnknownModuleError struct {
	Name       string
	Want       Kind
	Registered []Kind
}

func (e *UnknownModuleError) Error() string {
	if len(e.Registered) == 0 {
		return fmt.Sprintf("registry: no %s registered as %q", e.Want, e.Name)
	}

	names := make([]string, 0, len(e.Registered))
	for _, kind := range e.Registered {
		names = append(names, kind.String())
	}
	return fmt.Sprintf("registry: %q is a %s, not a %s", e.Name, strings.Join(names, " and a "), e.Want)
}
