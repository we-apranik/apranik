// Package match holds the lookup structures routing rules are compiled into:
// domains, IP ranges and ports. Each is built once and then only read, so one
// value serves every connection at once without locks.
package match

import (
	"regexp"
	"strings"
)

// DomainList is a set of domain patterns, one list per kind of match.
type DomainList struct {
	// The whole name, and nothing else.
	Exact []string

	// The name or any name under it: "google.com" matches "google.com" and
	// "mail.google.com", never "notgoogle.com".
	Suffixes []string

	// Anywhere in the name.
	Keywords []string

	Regexes []*regexp.Regexp
}

// DomainMatcher answers whether a name matches any pattern of a DomainList.
type DomainMatcher struct {
	exact    map[string]struct{}
	suffixes map[string]struct{}
	keywords []string
	regexes  []*regexp.Regexp
}

func NewDomainMatcher(list DomainList) *DomainMatcher {
	matcher := &DomainMatcher{
		exact:    make(map[string]struct{}, len(list.Exact)),
		suffixes: make(map[string]struct{}, len(list.Suffixes)),
		regexes:  list.Regexes,
	}
	for _, name := range list.Exact {
		if normalized := NormalizeDomain(name); normalized != "" {
			matcher.exact[normalized] = struct{}{}
		}
	}
	for _, suffix := range list.Suffixes {
		if normalized := NormalizeDomain(strings.TrimLeft(suffix, ".")); normalized != "" {
			matcher.suffixes[normalized] = struct{}{}
		}
	}
	for _, keyword := range list.Keywords {
		if normalized := strings.ToLower(keyword); normalized != "" {
			matcher.keywords = append(matcher.keywords, normalized)
		}
	}
	return matcher
}

// Match takes a name already passed through NormalizeDomain.
func (m *DomainMatcher) Match(normalizedName string) bool {
	if normalizedName == "" {
		return false
	}
	if _, found := m.exact[normalizedName]; found {
		return true
	}
	if len(m.suffixes) > 0 && m.matchSuffix(normalizedName) {
		return true
	}
	for _, keyword := range m.keywords {
		if strings.Contains(normalizedName, keyword) {
			return true
		}
	}
	for _, regex := range m.regexes {
		if regex.MatchString(normalizedName) {
			return true
		}
	}
	return false
}

// One map lookup per label, from the whole name down to the last label, so a
// suffix only ever matches at a label boundary.
func (m *DomainMatcher) matchSuffix(name string) bool {
	for {
		if _, found := m.suffixes[name]; found {
			return true
		}
		dot := strings.IndexByte(name, '.')
		if dot < 0 {
			return false
		}
		name = name[dot+1:]
	}
}

// Len reports how many patterns the matcher holds.
func (m *DomainMatcher) Len() int {
	return len(m.exact) + len(m.suffixes) + len(m.keywords) + len(m.regexes)
}

// NormalizeDomain lowercases a name and drops a trailing dot, so
// "Example.COM." and "example.com" are the same name. It allocates only when
// the name has an upper-case letter.
func NormalizeDomain(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}
