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
