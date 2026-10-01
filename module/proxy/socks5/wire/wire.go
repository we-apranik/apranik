// Package wire is the SOCKS5 byte format from RFC 1928 and RFC 1929:
// constants and the reply writer. The address codec is shared/socksaddr. It knows nothing about
// carriers or callers.
package wire

import (
	"fmt"
	"io"

	"github.com/The-Apranik/Apranik/shared/socksaddr"
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

	ReplySucceeded           = 0x00
	ReplyGeneralFailure      = 0x01
	ReplyNotAllowed          = 0x02
	ReplyNetworkUnreachable  = 0x03
	ReplyHostUnreachable     = 0x04
	ReplyConnectionRefused   = 0x05
	ReplyTTLExpired          = 0x06
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
	case ReplyNotAllowed:
		return "connection not allowed by ruleset"
	case ReplyNetworkUnreachable:
		return "network unreachable"
	case ReplyHostUnreachable:
		return "host unreachable"
	case ReplyConnectionRefused:
		return "connection refused"
	case ReplyTTLExpired:
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

// WriteReply sends a reply carrying a bound address. A zero one becomes
// 0.0.0.0:0, which is what CONNECT wants: the field only means something for
// BIND and ASSOCIATE.
func WriteReply(writer io.Writer, code byte, bound spec.Endpoint) error {
	reply := []byte{Version5, code, 0x00}

	if !bound.IsValid() {
		reply = append(reply, socksaddr.AddressIPv4, 0, 0, 0, 0, 0, 0)
	} else {
		var err error
		if reply, err = socksaddr.AppendAddress(reply, bound.Destination()); err != nil {
			return err
		}
	}

	_, err := writer.Write(reply)
	return err
}
