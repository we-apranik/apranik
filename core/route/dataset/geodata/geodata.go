// Package geodata reads V2Ray's geosite.dat and geoip.dat: protobuf files holding many named categories ("ir", "category-ads-all"), each a list of domains or of IP ranges.
package geodata

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/we-apranik/apranik/core/route/router/match"
)

const (
	domainTypeSubstring = 0
	domainTypeRegex     = 1
	domainTypeDomain    = 2
	domainTypeFull      = 3
)

var (
	ErrCategoryNotFound = errors.New("geodata: category not found")
	ErrWrongKind        = errors.New("geodata: the file holds the other kind of list")
	errMalformed        = errors.New("geodata: malformed file")
)

func ReadDomains(file []byte, category, attribute string) (match.DomainList, error) {
	entry, err := findCategory(file, category)
	if err != nil {
		return match.DomainList{}, err
	}

	var list match.DomainList
	err = forEachField(entry, func(number int, wireType int, value []byte, _ uint64) error {
		if number != 2 {
			return nil
		}
		if wireType != wireTypeBytes {
			return errMalformed
		}
		domain, err := decodeDomain(value)
		if err != nil {
			return err
		}
		if attribute != "" && !domain.hasAttribute(attribute) {
			return nil
		}
		return domain.addTo(&list)
	})
	return list, err
}

func ReadPrefixes(file []byte, category string) ([]netip.Prefix, error) {
	entry, err := findCategory(file, category)
	if err != nil {
		return nil, err
	}

	var prefixes []netip.Prefix
	err = forEachField(entry, func(number int, wireType int, value []byte, _ uint64) error {
		if number != 2 {
			return nil
		}
		if wireType != wireTypeBytes {
			return errMalformed
		}
		prefix, err := decodeCIDR(value)
		if err != nil {
			return err
		}
		prefixes = append(prefixes, prefix)
		return nil
	})
	return prefixes, err
}

func findCategory(file []byte, category string) ([]byte, error) {
	var found []byte
	err := forEachField(file, func(number int, wireType int, entry []byte, _ uint64) error {
		if number != 1 || found != nil {
			return nil
		}
		if wireType != wireTypeBytes {
			return errMalformed
		}
		code, err := readCode(entry)
		if err != nil {
			return err
		}
		if strings.EqualFold(code, category) {
			found = entry
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("%w: %q", ErrCategoryNotFound, category)
	}
	return found, nil
}

func readCode(entry []byte) (string, error) {
	var code string
	err := forEachField(entry, func(number int, wireType int, value []byte, _ uint64) error {
		if number == 1 && wireType == wireTypeBytes {
			code = string(value)
		}
		return nil
	})
	return code, err
}

type domain struct {
	matchType  uint64
	value      string
	attributes [][]byte
}

func decodeDomain(message []byte) (domain, error) {
	var decoded domain
	err := forEachField(message, func(number int, wireType int, value []byte, varint uint64) error {
		switch {
		case number == 1 && wireType == wireTypeVarint:
			decoded.matchType = varint
		case number == 2 && wireType == wireTypeBytes:
			decoded.value = string(value)
		case number == 3 && wireType == wireTypeBytes:
			decoded.attributes = append(decoded.attributes, value)
		case number == 1 && wireType == wireTypeBytes, number == 2 && wireType == wireTypeVarint:
			return ErrWrongKind
		}
		return nil
	})
	return decoded, err
}

func (d domain) hasAttribute(wanted string) bool {
	for _, attribute := range d.attributes {
		found := false
		_ = forEachField(attribute, func(number int, wireType int, value []byte, _ uint64) error {
			if number == 1 && wireType == wireTypeBytes && strings.EqualFold(string(value), wanted) {
				found = true
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}

func (d domain) addTo(list *match.DomainList) error {
	if d.value == "" {
		return nil
	}
	switch d.matchType {
	case domainTypeSubstring:
		list.Keywords = append(list.Keywords, d.value)
	case domainTypeRegex:
		regex, err := regexp.Compile(d.value)
		if err != nil {
			return fmt.Errorf("geodata: entry %q: %w", d.value, err)
		}
		list.Regexes = append(list.Regexes, regex)
	case domainTypeDomain:
		list.Suffixes = append(list.Suffixes, d.value)
	case domainTypeFull:
		list.Exact = append(list.Exact, d.value)
	default:
		return fmt.Errorf("%w: unknown domain type %d", errMalformed, d.matchType)
	}
	return nil
}

func decodeCIDR(message []byte) (netip.Prefix, error) {
	var (
		address   []byte
		prefixLen uint64
	)
	err := forEachField(message, func(number int, wireType int, value []byte, varint uint64) error {
		switch {
		case number == 1 && wireType == wireTypeBytes:
			address = value
		case number == 2 && wireType == wireTypeVarint:
			prefixLen = varint
		case number == 1 && wireType == wireTypeVarint, number == 2 && wireType == wireTypeBytes:
			return ErrWrongKind
		}
		return nil
	})
	if err != nil {
		return netip.Prefix{}, err
	}

	ip, ok := netip.AddrFromSlice(address)
	if !ok {
		return netip.Prefix{}, fmt.Errorf("%w: an IP of %d bytes", errMalformed, len(address))
	}
	prefix := netip.PrefixFrom(ip, int(min(prefixLen, 128)))
	if !prefix.IsValid() {
		return netip.Prefix{}, fmt.Errorf("%w: prefix /%d for %s", errMalformed, prefixLen, ip)
	}
	return prefix.Masked(), nil
}
