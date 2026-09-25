// Package wire is the SOCKS5 byte format from RFC 1928 and RFC 1929:
// constants, the address codec and the reply writer. It knows nothing about
// carriers or callers.
package wire

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"

	"github.com/The-Apranik/Apranik/spec"
)

const (
	Version5    = 0x05
	AuthVersion = 0x01

	MethodNoAuth   = 0x00
	MethodUserPass = 0x02
	MethodNone     = 0xFF

	AuthSucceeded = 0x00
	AuthFailed    = 0x01

	CommandConnect      = 0x01
	CommandBind         = 0x02
	CommandUDPAssociate = 0x03

	AddressIPv4   = 0x01
	AddressDomain = 0x03
	AddressIPv6   = 0x04

	ReplySucceeded           = 0x00
	ReplyGeneralFailure      = 0x01
	ReplyCommandNotSupported = 0x07
	ReplyAddressNotSupported = 0x08
)

// ReplyError is a SOCKS5 reply the server refused with, kept distinct so a
// caller can tell "command not supported" from "host unreachable".
type ReplyError struct {
	Code byte
}

func (e *ReplyError) Error() string {
	return fmt.Sprintf("socks5: server replied %s (X'%02X')", ReplyText(e.Code), e.Code)
}

func ReplyText(code byte) string {
	switch code {
	case ReplySucceeded:
		return "succeeded"
	case ReplyGeneralFailure:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case ReplyCommandNotSupported:
		return "command not supported"
	case ReplyAddressNotSupported:
		return "address type not supported"
	default:
		return "unassigned reply code"
	}
}

func CommandText(command byte) string {
	switch command {
	case CommandConnect:
		return "CONNECT"
	case CommandBind:
		return "BIND"
	case CommandUDPAssociate:
		return "UDP ASSOCIATE"
	default:
		return fmt.Sprintf("X'%02X'", command)
	}
}

// ReadAddress reads one type-prefixed address and its port. Reads are exact,
// never buffered: everything after the handshake is payload.
func ReadAddress(reader io.Reader) (spec.Destination, error) {
	var kind [1]byte
	if _, err := io.ReadFull(reader, kind[:]); err != nil {
		return spec.Destination{}, fmt.Errorf("socks5: reading address type: %w", err)
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
			return spec.Destination{}, fmt.Errorf("socks5: reading address: %w", err)
		}
		parsed, ok := netip.AddrFromSlice(raw)
		if !ok {
			return spec.Destination{}, fmt.Errorf("socks5: %d address bytes are not an IP", len(raw))
		}
		ip = parsed.Unmap()

	case AddressDomain:
		var length [1]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return spec.Destination{}, fmt.Errorf("socks5: reading domain length: %w", err)
		}
		if length[0] == 0 {
			return spec.Destination{}, fmt.Errorf("socks5: empty domain name")
		}
		raw := make([]byte, length[0])
		if _, err := io.ReadFull(reader, raw); err != nil {
			return spec.Destination{}, fmt.Errorf("socks5: reading domain: %w", err)
		}
		domain = string(raw)

	default:
		return spec.Destination{}, fmt.Errorf("socks5: address type X'%02X' not supported", kind[0])
	}

	var port [2]byte
	if _, err := io.ReadFull(reader, port[:]); err != nil {
		return spec.Destination{}, fmt.Errorf("socks5: reading port: %w", err)
	}

	if domain != "" {
		return spec.NewDestinationFromDomain(domain, binary.BigEndian.Uint16(port[:]))
	}
	return spec.NewDestinationFromIP(ip, binary.BigEndian.Uint16(port[:]))
}

func AppendAddress(destination []byte, address spec.Destination) ([]byte, error) {
	switch domain, isDomain := address.Domain(); {
	case isDomain:
		if len(domain) > 255 {
			return nil, fmt.Errorf("socks5: domain %q is %d bytes, the wire format allows 255", domain, len(domain))
		}
		destination = append(destination, AddressDomain, byte(len(domain)))
		destination = append(destination, domain...)

	default:
		ip, ok := address.IP()
		if !ok {
			return nil, fmt.Errorf("socks5: address has neither domain nor IP")
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

// WriteReply sends a reply carrying a bound address. A zero one becomes
// 0.0.0.0:0, which is what CONNECT wants: the field only means something for
// BIND and ASSOCIATE.
func WriteReply(writer io.Writer, code byte, bound spec.Endpoint) error {
	reply := []byte{Version5, code, 0x00}

	if !bound.IsValid() {
		reply = append(reply, AddressIPv4, 0, 0, 0, 0, 0, 0)
	} else {
		var err error
		if reply, err = AppendAddress(reply, bound.Destination()); err != nil {
			return err
		}
	}

	_, err := writer.Write(reply)
	return err
}
