package registry

import (
	"slices"
	"strings"

	"github.com/we-apranik/apranik/spec"
)

type Listing struct {
	Name        string
	Kind        Kind
	Description string
}

// index backs error text, the listing and the transport keyspace rule -- never
// dispatch. Descriptions are captured at registration so listing never has to
// construct a module or switch on a kind.
type index struct {
	kindsByName map[string][]Kind
	entries     []Listing
}

func (i *index) kinds(name string) []Kind { return i.kindsByName[name] }

func (i *index) record(info spec.ModuleInfo, kind Kind) {
	if i.kindsByName == nil {
		i.kindsByName = make(map[string][]Kind)
	}
	i.kindsByName[info.Name] = append(i.kindsByName[info.Name], kind)
	i.entries = append(i.entries, Listing{
		Name:        info.Name,
		Kind:        kind,
		Description: info.Description,
	})
}

// Sorted: registration order is a property of default.go, not something a user
// should see.
func (i *index) listings() []Listing {
	sorted := slices.Clone(i.entries)
	slices.SortFunc(sorted, func(first, second Listing) int {
		if first.Name != second.Name {
			return strings.Compare(first.Name, second.Name)
		}
		return int(first.Kind) - int(second.Kind)
	})
	return sorted
}
