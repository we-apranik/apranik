// Package vmessaddr is the destination layout of the V2Ray family: the port
// first, then a type byte, then the address.
package vmessaddr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"

	"github.com/we-apranik/apranik/spec"
)

const (
	AddressIPv4   = 0x01
	AddressDomain = 0x02
	AddressIPv6   = 0x03
)

const MaxDomainLength = 255

var (
	ErrEmptyDomain            = errors.New("vmessaddr: empty domain name")
	ErrUnsupportedAddressType = errors.New("vmessaddr: address type not supported")
	ErrDomainTooLong          = errors.New("vmessaddr: domain longer than 255 bytes")
)

// ReadAddress reads one port and address. Reads are exact, never buffered, so
// whatever follows stays unread for the caller. A reader that ends before the
func ReadAddress(reader io.Reader) (spec.Destination, error) {
	var portAndType [3]byte
	if _, err := io.ReadFull(reader, portAndType[:]); err != nil {
		return spec.Destination{}, fmt.Errorf("vmessaddr: reading port and address type: %w", err)
	}
	port := binary.BigEndian.Uint16(portAndType[:2])

	switch portAndType[2] {
	case AddressIPv4, AddressIPv6:
		length := net.IPv4len
		if portAndType[2] == AddressIPv6 {
			length = net.IPv6len
		}
		raw := make([]byte, length)
		if _, err := io.ReadFull(reader, raw); err != nil {
			return spec.Destination{}, fmt.Errorf("vmessaddr: reading address: %w", unexpectedIfEnded(err))
		}
		ip, _ := netip.AddrFromSlice(raw)
		return spec.NewDestinationFromIP(ip, port)

	case AddressDomain:
		var length [1]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return spec.Destination{}, fmt.Errorf("vmessaddr: reading domain length: %w", unexpectedIfEnded(err))
		}
		if length[0] == 0 {
			return spec.Destination{}, ErrEmptyDomain
		}
		raw := make([]byte, length[0])
		if _, err := io.ReadFull(reader, raw); err != nil {
			return spec.Destination{}, fmt.Errorf("vmessaddr: reading domain: %w", unexpectedIfEnded(err))
		}
		return spec.NewDestinationFromDomain(string(raw), port)

	default:
		return spec.Destination{}, fmt.Errorf("%w: X'%02X'", ErrUnsupportedAddressType, portAndType[2])
	}
}

func ParseAddress(encoded []byte) (spec.Destination, int, error) {
	if len(encoded) < 3 {
		return spec.Destination{}, 0, fmt.Errorf("vmessaddr: reading port and address type: %w", io.ErrUnexpectedEOF)
	}
	port := binary.BigEndian.Uint16(encoded)

	switch encoded[2] {
	case AddressIPv4:
		if len(encoded) < 3+net.IPv4len {
			return spec.Destination{}, 0, fmt.Errorf("vmessaddr: reading address: %w", io.ErrUnexpectedEOF)
		}
		destination, err := spec.NewDestinationFromIP(netip.AddrFrom4([net.IPv4len]byte(encoded[3:])), port)
		return destination, 3 + net.IPv4len, err

	case AddressIPv6:
		if len(encoded) < 3+net.IPv6len {
			return spec.Destination{}, 0, fmt.Errorf("vmessaddr: reading address: %w", io.ErrUnexpectedEOF)
		}
		destination, err := spec.NewDestinationFromIP(netip.AddrFrom16([net.IPv6len]byte(encoded[3:])), port)
		return destination, 3 + net.IPv6len, err

	case AddressDomain:
		if len(encoded) < 4 {
			return spec.Destination{}, 0, fmt.Errorf("vmessaddr: reading domain length: %w", io.ErrUnexpectedEOF)
		}
		length := int(encoded[3])
		if length == 0 {
			return spec.Destination{}, 0, ErrEmptyDomain
		}
		if len(encoded) < 4+length {
			return spec.Destination{}, 0, fmt.Errorf("vmessaddr: reading domain: %w", io.ErrUnexpectedEOF)
		}
		destination, err := spec.NewDestinationFromDomain(string(encoded[4:4+length]), port)
		return destination, 4 + length, err

	default:
		return spec.Destination{}, 0, fmt.Errorf("%w: X'%02X'", ErrUnsupportedAddressType, encoded[2])
	}
}

func AppendAddress(buffer []byte, destination spec.Destination) ([]byte, error) {
	if ip, isIP := destination.IP(); isIP {
		buffer = binary.BigEndian.AppendUint16(buffer, destination.Port())
		if ip.Is4() {
			four := ip.As4()
			buffer = append(buffer, AddressIPv4)
			return append(buffer, four[:]...), nil
		}
		sixteen := ip.As16()
		buffer = append(buffer, AddressIPv6)
		return append(buffer, sixteen[:]...), nil
	}

	domain, isDomain := destination.Domain()
	if !isDomain {
		return nil, errors.New("vmessaddr: destination has neither a domain nor an IP")
	}
	if len(domain) > MaxDomainLength {
		return nil, fmt.Errorf("%w: %d bytes", ErrDomainTooLong, len(domain))
	}
	buffer = binary.BigEndian.AppendUint16(buffer, destination.Port())
	buffer = append(buffer, AddressDomain, byte(len(domain)))
	return append(buffer, domain...), nil
}

func unexpectedIfEnded(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
