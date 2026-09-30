// Package dataset loads the named lists routing rules look up. A
// dataset's source (where the bytes come from) is apart from its format (how
// they are read), so a new format is one reader and a new source, such as a
// URL, touches no format.
package dataset

import (
	"errors"
	"fmt"
	"os"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/route/dataset/geodata"
	"github.com/The-Apranik/Apranik/core/route/match"
)

// Dataset is one loaded list: Domains for a domain dataset, IPs for an IP one.
type Dataset struct {
	Kind    blueprint.DatasetKind
	Domains *match.DomainMatcher
	IPs     *match.PrefixMatcher
}

// Len reports how many entries were loaded.
func (d *Dataset) Len() int {
	if d.Kind == blueprint.DatasetKindDomain {
		return d.Domains.Len()
	}
	return d.IPs.Len()
}

// LoadAll loads every declaration, reading each file once however many
// datasets name it. errs has one entry per declaration, nil where it loaded,
// so a caller can report each failure where it was declared.
func LoadAll(declarations []blueprint.Dataset) (loaded map[string]*Dataset, errs []error) {
	loaded = make(map[string]*Dataset, len(declarations))
	errs = make([]error, len(declarations))
	files := make(map[string]fileContent)

	for index, declaration := range declarations {
		content, read := files[declaration.File]
		if !read {
			content.bytes, content.err = os.ReadFile(declaration.File)
			files[declaration.File] = content
		}
		if content.err != nil {
			errs[index] = content.err
			continue
		}
		dataset, err := decode(declaration, content.bytes)
		if err != nil {
			errs[index] = fmt.Errorf("%s: %w", declaration.File, err)
			continue
		}
		loaded[declaration.Tag] = dataset
	}
	return loaded, errs
}

// Joined returns LoadAll's errors as one, naming each dataset, or nil.
func Joined(declarations []blueprint.Dataset, errs []error) error {
	var named []error
	for index, err := range errs {
		if err != nil {
			named = append(named, fmt.Errorf("dataset %q: %w", declarations[index].Tag, err))
		}
	}
	return errors.Join(named...)
}

type fileContent struct {
	bytes []byte
	err   error
}

func decode(declaration blueprint.Dataset, content []byte) (*Dataset, error) {
	if declaration.Format != blueprint.DatasetFormatGeodata {
		return nil, fmt.Errorf("unknown format %q", declaration.Format)
	}

	switch declaration.Kind {
	case blueprint.DatasetKindDomain:
		list, err := geodata.ReadDomains(content, declaration.Category, declaration.Attribute)
		if errors.Is(err, geodata.ErrWrongKind) {
			return nil, errors.New("the file holds IP ranges, but kind is \"domain\"")
		}
		if err != nil {
			return nil, err
		}
		return &Dataset{Kind: declaration.Kind, Domains: match.NewDomainMatcher(list)}, nil
	case blueprint.DatasetKindIP:
		if declaration.Attribute != "" {
			return nil, errors.New("attribute applies to domain datasets only")
		}
		prefixes, err := geodata.ReadPrefixes(content, declaration.Category)
		if errors.Is(err, geodata.ErrWrongKind) {
			return nil, errors.New("the file holds domains, but kind is \"ip\"")
		}
		if err != nil {
			return nil, err
		}
		return &Dataset{Kind: declaration.Kind, IPs: match.NewPrefixMatcher(prefixes)}, nil
	default:
		return nil, fmt.Errorf("unknown kind %q", declaration.Kind)
	}
}
