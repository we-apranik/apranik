// Package wire is the VLESS byte format: the request a client sends once at
// the start of a stream, the answer the server puts in front of its first
// payload, and the frame that carries each UDP datagram. It knows nothing about
// carriers or callers.
//
//	request:  VERSION UUID ADDONS-LENGTH ADDONS COMMAND [PORT ATYP ADDR]
//	response: VERSION ADDONS-LENGTH ADDONS
//	UDP frame: LENGTH PAYLOAD
//
// PORT comes before the address, and ATYP is 1 IPv4, 2 domain, 3 IPv6, unlike
// SOCKS5; that layout is shared/vmessaddr, which this package uses. A Mux or
// Reverse command carries no port and no address. The destination of a UDP command is named
// once, in the request; the frames after it name nothing.
//
// Checked against the VLESS page of the Xray docs, Xray-core
// (proxy/vless/encoding, common/protocol/address.go) and sing-vmess (vless).
package wire

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/The-Apranik/Apranik/shared/vmessaddr"
	"github.com/The-Apranik/Apranik/spec"
)

const Version = 0

const (
	CommandTCP     = 0x01
	CommandUDP     = 0x02
	CommandMux     = 0x03
	CommandReverse = 0x04
)

const UUIDLength = 16

// RequestPrefixLength is version, UUID and the addons length: the bytes that
// come before the addons, whose length they give.
const RequestPrefixLength = 1 + UUIDLength + 1

const MaxDomainLength = vmessaddr.MaxDomainLength

// MaxUDPPayload is what the two-byte length field can express.
const MaxUDPPayload = 0xFFFF

// The Flow addons key and the one Xray and sing-box know besides it.
const (
	addonsFlowTag = 1<<3 | 2
	addonsSeedTag = 2<<3 | 2
)

var (
	ErrUnknownVersion         = errors.New("vless: unknown version")
	ErrUnknownCommand         = errors.New("vless: unknown command")
	ErrMalformedAddons        = errors.New("vless: malformed addons")
	ErrUnsupportedAddressType = vmessaddr.ErrUnsupportedAddressType
	ErrEmptyDomain            = vmessaddr.ErrEmptyDomain
	ErrDomainTooLong          = vmessaddr.ErrDomainTooLong
	ErrInvalidUUID            = errors.New("vless: invalid UUID")
	ErrMuxNamesNoDestination  = errors.New("vless: a Mux request names no destination")
)

// UUID is the credential of a user, exactly as it appears on the wire. Never
// log or format it.
type UUID [UUIDLength]byte

// Format keeps a UUID out of logs and error text under every verb, as
// types.Secret does for passwords.
func (UUID) Format(state fmt.State, verb rune) {
	if verb == 'q' {
		fmt.Fprintf(state, "%q", "[redacted]")
		return
	}
	io.WriteString(state, "[redacted]")
}

// ParseUUID reads the text a user writes for an ID the way Xray and sing-box
// do: 32 to 36 characters are a UUID, with or without dashes; 1 to 30 bytes of
// anything else are turned into a UUID version 5 in the nil namespace. 31
// bytes, 37 or more, and nothing at all are refused, as Xray refuses them.
func ParseUUID(text string) (UUID, error) {
	length := len(text)
	if length == 0 || length > 36 || length == 31 {
		return UUID{}, ErrInvalidUUID
	}
	if length < 32 {
		return uuidVersion5InNilNamespace(text), nil
	}

	withoutDashes := make([]byte, 0, 32)
	for index := 0; index < length; index++ {
		character := text[index]
		if character == '-' {
			if !isDashPosition(index, length) {
				return UUID{}, ErrInvalidUUID
			}
			continue
		}
		withoutDashes = append(withoutDashes, character)
	}
	var parsed UUID
	if len(withoutDashes) != hex.EncodedLen(UUIDLength) {
		return UUID{}, ErrInvalidUUID
	}
	if _, err := hex.Decode(parsed[:], withoutDashes); err != nil {
		return UUID{}, ErrInvalidUUID
	}
	return parsed, nil
}

// Dashes are allowed only where the canonical form has them, and either all
// of them or none.
func isDashPosition(index, length int) bool {
	if length != 36 {
		return false
	}
	return index == 8 || index == 13 || index == 18 || index == 23
}

func uuidVersion5InNilNamespace(text string) UUID {
	hash := sha1.New()
	hash.Write(make([]byte, UUIDLength))
	hash.Write([]byte(text))
	var derived UUID
	copy(derived[:], hash.Sum(nil))
	derived[6] = derived[6]&0x0f | 5<<4
	derived[8] = derived[8]&0x3f | 0x80
	return derived
}

type Request struct {
	UUID UUID

	// Empty, or what the client asked for: "xtls-rprx-vision" is the one flow
	// that exists. The module supports none and refuses any.
	Flow string

	Command byte

	// Zero for Mux and Reverse, which name none.
	Destination spec.Destination
}

// AppendRequestHeader encodes the header for a TCP, UDP or Mux command, refusing a
// destination the address format cannot carry before anything reaches the wire.
// A Mux request carries no port and no address, so it must be given none.
func AppendRequestHeader(destination []byte, request Request) ([]byte, error) {
	switch request.Command {
	case CommandTCP, CommandUDP:
	case CommandMux:
		if request.Destination.IsValid() {
			return nil, ErrMuxNamesNoDestination
		}
	default:
		return nil, fmt.Errorf("%w: X'%02X'", ErrUnknownCommand, request.Command)
	}
	addons, err := appendAddons(nil, request.Flow)
	if err != nil {
		return nil, err
	}

	destination = append(destination, Version)
	destination = append(destination, request.UUID[:]...)
	destination = append(destination, byte(len(addons)))
	destination = append(destination, addons...)
	destination = append(destination, request.Command)
	if request.Command == CommandMux {
		return destination, nil
	}
	return vmessaddr.AppendAddress(destination, request.Destination)
}

// ReadRequestHeader reads exactly one header and nothing after it, so the payload
// that follows in the same segment stays in the reader.
func ReadRequestHeader(reader io.Reader) (Request, error) {
	var prefix [RequestPrefixLength]byte
	if _, err := io.ReadFull(reader, prefix[:]); err != nil {
		return Request{}, fmt.Errorf("vless: reading request: %w", err)
	}
	if prefix[0] != Version {
		return Request{}, fmt.Errorf("%w: %d", ErrUnknownVersion, prefix[0])
	}

	var request Request
	copy(request.UUID[:], prefix[1:1+UUIDLength])

	addons := make([]byte, prefix[RequestPrefixLength-1])
	if _, err := io.ReadFull(reader, addons); err != nil {
		return Request{}, fmt.Errorf("vless: reading addons: %w", err)
	}
	flow, err := parseAddons(addons)
	if err != nil {
		return Request{}, err
	}
	request.Flow = flow

	var command [1]byte
	if _, err := io.ReadFull(reader, command[:]); err != nil {
		return Request{}, fmt.Errorf("vless: reading command: %w", err)
	}
	request.Command = command[0]

	switch request.Command {
	case CommandTCP, CommandUDP:
		request.Destination, err = vmessaddr.ReadAddress(reader)
		if err != nil {
			return Request{}, fmt.Errorf("vless: reading destination: %w", unexpectedIfEnded(err))
		}
	case CommandMux, CommandReverse:
	default:
		return Request{}, fmt.Errorf("%w: X'%02X'", ErrUnknownCommand, request.Command)
	}
	return request, nil
}

// CommandOffset reports where the command byte sits in a request that begins
// with prefix, which must hold at least RequestPrefixLength bytes: only the
// addons, whose length the prefix gives, come before it. io.ErrUnexpectedEOF
// means prefix is too short to tell yet.
func CommandOffset(prefix []byte) (int, error) {
	if len(prefix) < RequestPrefixLength {
		return 0, io.ErrUnexpectedEOF
	}
	return RequestPrefixLength + int(prefix[RequestPrefixLength-1]), nil
}

// AppendResponseHeader encodes what the server puts in front of its first
// payload: the version and no addons.
func AppendResponseHeader(destination []byte) []byte {
	return append(destination, Version, 0)
}

// ReadResponseHeader reads the server's header and drops its addons, which
// Xray and sing-box clients ignore as well.
func ReadResponseHeader(reader io.Reader) error {
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return fmt.Errorf("vless: reading response: %w", err)
	}
	if header[0] != Version {
		return fmt.Errorf("%w: %d", ErrUnknownVersion, header[0])
	}
	if _, err := io.CopyN(io.Discard, reader, int64(header[1])); err != nil {
		return fmt.Errorf("vless: reading response addons: %w", unexpectedIfEnded(err))
	}
	return nil
}

// AppendUDPFrame encodes one datagram. A payload longer than the length field
// can express is spec.ErrMessageTooLong, not a truncation.
func AppendUDPFrame(destination []byte, payload []byte) ([]byte, error) {
	if len(payload) > MaxUDPPayload {
		return nil, fmt.Errorf("vless: %d byte datagram: %w", len(payload), spec.ErrMessageTooLong)
	}
	destination = binary.BigEndian.AppendUint16(destination, uint16(len(payload)))
	return append(destination, payload...), nil
}

// ReadUDPFrame reads one frame into payload. A frame larger than payload is
// read to its end and dropped, so the stream stays aligned on the next frame,
// and reported as spec.ErrMessageTooLong. A stream that ends before a frame
// starts ends cleanly with io.EOF; one that ends inside a frame is
// io.ErrUnexpectedEOF.
func ReadUDPFrame(reader *bufio.Reader, payload []byte) (int, error) {
	var length [2]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return 0, fmt.Errorf("vless: reading datagram length: %w", err)
	}
	size := int(binary.BigEndian.Uint16(length[:]))

	if size > len(payload) {
		if _, err := io.CopyN(io.Discard, reader, int64(size)); err != nil {
			return 0, fmt.Errorf("vless: skipping datagram: %w", unexpectedIfEnded(err))
		}
		return 0, fmt.Errorf("vless: %d byte datagram into a %d byte buffer: %w", size, len(payload), spec.ErrMessageTooLong)
	}
	if _, err := io.ReadFull(reader, payload[:size]); err != nil {
		return 0, fmt.Errorf("vless: reading datagram: %w", unexpectedIfEnded(err))
	}
	return size, nil
}

func unexpectedIfEnded(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// The addons are a protobuf message: Flow is field 1 and Seed field 2, both
// length-delimited. Only Flow is kept. A field this module does not know is
// refused, as sing-box does, so a request it cannot read is never half
// understood.
func parseAddons(addons []byte) (string, error) {
	reader := bytes.NewReader(addons)
	var flow string
	for reader.Len() > 0 {
		tag, err := binary.ReadUvarint(reader)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrMalformedAddons, err)
		}
		if tag != addonsFlowTag && tag != addonsSeedTag {
			return "", fmt.Errorf("%w: unknown field header %d", ErrMalformedAddons, tag)
		}
		length, err := binary.ReadUvarint(reader)
		if err != nil || length > uint64(reader.Len()) {
			return "", fmt.Errorf("%w: field longer than the addons", ErrMalformedAddons)
		}
		value := make([]byte, length)
		reader.Read(value)
		if tag == addonsFlowTag {
			flow = string(value)
		}
	}
	return flow, nil
}

func appendAddons(destination []byte, flow string) ([]byte, error) {
	if flow == "" {
		return destination, nil
	}
	destination = append(destination, addonsFlowTag)
	destination = binary.AppendUvarint(destination, uint64(len(flow)))
	destination = append(destination, flow...)
	if len(destination) > 0xFF {
		return nil, fmt.Errorf("%w: %d bytes do not fit the one-byte length", ErrMalformedAddons, len(destination))
	}
	return destination, nil
}
