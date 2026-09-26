// Package socksaddr is the type-prefixed address from RFC 1928 section 5:
// ATYP, then the address, then a big-endian port. SOCKS5 and Trojan both put
// destinations on the wire this way.
package socksaddr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"

	"github.com/The-Apranik/Apranik/spec"
)

const (
	AddressIPv4   = 0x01
	AddressDomain = 0x03
	AddressIPv6   = 0x04
)

const MaxDomainLength = 255

var (
	ErrEmptyDomain            = errors.New("socksaddr: empty domain name")
	ErrUnsupportedAddressType = errors.New("socksaddr: address type not supported")
	ErrDomainTooLong          = errors.New("socksaddr: domain longer than 255 bytes")
)

// ReadAddress reads one address and its port. Reads are exact, never
// buffered, so whatever follows stays unread for the caller.
func ReadAddress(reader io.Reader) (spec.Destination, error) {
	var kind [1]byte
	if _, err := io.ReadFull(reader, kind[:]); err != nil {
		return spec.Destination{}, fmt.Errorf("socksaddr: reading address type: %w", err)
	}

	var (
		ip     netip.Addr
		domain string
	)
	switch kind[0] {
	case AddressIPv4, AddressIPv6:
		length := net.IPv4len
		if kind[0] == AddressIPv6 {
			length = net.IPv6len
		}
		raw := make([]byte, length)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return spec.Destination{}, fmt.Errorf("socksaddr: reading address: %w", err)
		}
		parsed, _ := netip.AddrFromSlice(raw)
		ip = parsed.Unmap()

	case AddressDomain:
		var length [1]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return spec.Destination{}, fmt.Errorf("socksaddr: reading domain length: %w", err)
		}
		if length[0] == 0 {
			return spec.Destination{}, ErrEmptyDomain
		}
		raw := make([]byte, length[0])
		if _, err := io.ReadFull(reader, raw); err != nil {
			return spec.Destination{}, fmt.Errorf("socksaddr: reading domain: %w", err)
		}
		domain = string(raw)

	default:
		return spec.Destination{}, fmt.Errorf("%w: X'%02X'", ErrUnsupportedAddressType, kind[0])
	}

	var port [2]byte
	if _, err := io.ReadFull(reader, port[:]); err != nil {
		return spec.Destination{}, fmt.Errorf("socksaddr: reading port: %w", err)
	}

	if domain != "" {
		return spec.NewDestinationFromDomain(domain, binary.BigEndian.Uint16(port[:]))
	}
	return spec.NewDestinationFromIP(ip, binary.BigEndian.Uint16(port[:]))
}

// ParseAddress decodes one address and its port from the start of encoded and
// reports how many bytes that took. It copies nothing for an IP, so a caller
// holding a whole packet parses it without allocating.
func ParseAddress(encoded []byte) (spec.Destination, int, error) {
	if len(encoded) == 0 {
		return spec.Destination{}, 0, fmt.Errorf("socksaddr: reading address type: %w", io.ErrUnexpectedEOF)
	}

	var (
		ip     netip.Addr
		domain string
		offset int
	)
	switch encoded[0] {
	case AddressIPv4:
		if len(encoded) < 1+net.IPv4len+2 {
			return spec.Destination{}, 0, fmt.Errorf("socksaddr: reading address: %w", io.ErrUnexpectedEOF)
		}
		ip = netip.AddrFrom4([net.IPv4len]byte(encoded[1:]))
		offset = 1 + net.IPv4len

	case AddressIPv6:
		if len(encoded) < 1+net.IPv6len+2 {
			return spec.Destination{}, 0, fmt.Errorf("socksaddr: reading address: %w", io.ErrUnexpectedEOF)
		}
		ip = netip.AddrFrom16([net.IPv6len]byte(encoded[1:])).Unmap()
		offset = 1 + net.IPv6len

	case AddressDomain:
		if len(encoded) < 2 {
			return spec.Destination{}, 0, fmt.Errorf("socksaddr: reading domain length: %w", io.ErrUnexpectedEOF)
		}
		length := int(encoded[1])
		if length == 0 {
			return spec.Destination{}, 0, ErrEmptyDomain
		}
		if len(encoded) < 2+length+2 {
			return spec.Destination{}, 0, fmt.Errorf("socksaddr: reading domain: %w", io.ErrUnexpectedEOF)
		}
		domain = string(encoded[2 : 2+length])
		offset = 2 + length

	default:
		return spec.Destination{}, 0, fmt.Errorf("%w: X'%02X'", ErrUnsupportedAddressType, encoded[0])
	}

	port := binary.BigEndian.Uint16(encoded[offset:])
	var (
		destination spec.Destination
		err         error
	)
	if domain != "" {
		destination, err = spec.NewDestinationFromDomain(domain, port)
	} else {
		destination, err = spec.NewDestinationFromIP(ip, port)
	}
	return destination, offset + 2, err
}

func AppendAddress(destination []byte, address spec.Destination) ([]byte, error) {
	switch domain, isDomain := address.Domain(); {
	case isDomain:
		if len(domain) > MaxDomainLength {
			return nil, fmt.Errorf("%w: %q is %d bytes, the wire format allows 255", ErrDomainTooLong, domain, len(domain))
		}
		destination = append(destination, AddressDomain, byte(len(domain)))
		destination = append(destination, domain...)

	default:
		ip, ok := address.IP()
		if !ok {
			return nil, errors.New("socksaddr: address has neither domain nor IP")
		}
		if ip.Is4() {
			four := ip.As4()
			destination = append(destination, AddressIPv4)
			destination = append(destination, four[:]...)
			break
		}
		sixteen := ip.As16()
		destination = append(destination, AddressIPv6)
		destination = append(destination, sixteen[:]...)
	}

	return binary.BigEndian.AppendUint16(destination, address.Port()), nil
}
