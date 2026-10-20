// Package geodatatest writes small geosite.dat and geoip.dat files for tests, so no test depends on a binary blob checked into the repository.
package geodatatest

import (
	"encoding/binary"
	"net/netip"
)

const (
	Keyword = 0
	Regex   = 1
	Suffix  = 2
	Exact   = 3
)

type Domain struct {
	Type       int
	Value      string
	Attributes []string
}

func Site(categories map[string][]Domain) []byte {
	var file []byte
	for code, domains := range categories {
		entry := appendBytesField(nil, 1, []byte(code))
		for _, domain := range domains {
			message := appendVarintField(nil, 1, uint64(domain.Type))
			message = appendBytesField(message, 2, []byte(domain.Value))
			for _, attribute := range domain.Attributes {
				attributeMessage := appendBytesField(nil, 1, []byte(attribute))
				attributeMessage = appendVarintField(attributeMessage, 2, 1)
				message = appendBytesField(message, 3, attributeMessage)
			}
			entry = appendBytesField(entry, 2, message)
		}
		file = appendBytesField(file, 1, entry)
	}
	return file
}

func IP(categories map[string][]netip.Prefix) []byte {
	var file []byte
	for code, prefixes := range categories {
		entry := appendBytesField(nil, 1, []byte(code))
		for _, prefix := range prefixes {
			message := appendBytesField(nil, 1, prefix.Addr().AsSlice())
			message = appendVarintField(message, 2, uint64(prefix.Bits()))
			entry = appendBytesField(entry, 2, message)
		}
		file = appendBytesField(file, 1, entry)
	}
	return file
}

func appendVarintField(message []byte, number int, value uint64) []byte {
	message = binary.AppendUvarint(message, uint64(number)<<3)
	return binary.AppendUvarint(message, value)
}

func appendBytesField(message []byte, number int, value []byte) []byte {
	message = binary.AppendUvarint(message, uint64(number)<<3|2)
	message = binary.AppendUvarint(message, uint64(len(value)))
	return append(message, value...)
}
