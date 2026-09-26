// Package wire is the Trojan byte format: the request header a client sends
// once at the start of a stream, and the frame that carries each UDP datagram
// inside that stream. It knows nothing about carriers or callers.
//
//	request:   hex(SHA224(password)) CRLF CMD ATYP ADDR PORT CRLF
//	UDP frame: ATYP ADDR PORT LENGTH CRLF PAYLOAD
//
// There is no server reply: after the request the stream is raw payload.
package wire

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/The-Apranik/Apranik/shared/socksaddr"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	CommandConnect      = 0x01
	CommandUDPAssociate = 0x03
)

const PasswordHashLength = sha256.Size224 * 2

// MaxUDPPayload is what the two-byte length field can express.
const MaxUDPPayload = 0xFFFF

// PasswordHash is the lowercase hex SHA-224 of a password, exactly as it
// appears on the wire. It is a credential: never log or format it.
type PasswordHash [PasswordHashLength]byte

func HashPassword(password string) PasswordHash {
	sum := sha256.Sum224([]byte(password))
	var hash PasswordHash
	hex.Encode(hash[:], sum[:])
	return hash
}

// Format keeps a hash out of logs and error text under every verb, as
// types.Secret does for passwords.
func (PasswordHash) Format(state fmt.State, verb rune) {
	if verb == 'q' {
		fmt.Fprintf(state, "%q", "[redacted]")
		return
	}
	io.WriteString(state, "[redacted]")
}

var (
	ErrMalformedPasswordHash = errors.New("trojan: password hash is not 56 lowercase hex characters")
	ErrMissingCRLF           = errors.New("trojan: missing CRLF")
	ErrUnknownCommand        = errors.New("trojan: unknown command")
)

var crlf = [2]byte{'\r', '\n'}

type Request struct {
	PasswordHash PasswordHash
	Command      byte
	Destination  spec.Destination
}

// AppendRequest encodes the header, refusing a destination the address format
// cannot carry before anything reaches the wire.
func AppendRequest(destination []byte, request Request) ([]byte, error) {
	if request.Command != CommandConnect && request.Command != CommandUDPAssociate {
		return nil, fmt.Errorf("%w: X'%02X'", ErrUnknownCommand, request.Command)
	}
	destination = append(destination, request.PasswordHash[:]...)
	destination = append(destination, crlf[:]...)
	destination = append(destination, request.Command)
	destination, err := socksaddr.AppendAddress(destination, request.Destination)
	if err != nil {
		return nil, err
	}
	return append(destination, crlf[:]...), nil
}

// ReadRequest reads exactly one header and nothing after it, so the payload
// that follows in the same segment stays in the reader.
func ReadRequest(reader io.Reader) (Request, error) {
	var request Request
	if _, err := io.ReadFull(reader, request.PasswordHash[:]); err != nil {
		return Request{}, fmt.Errorf("trojan: reading password hash: %w", err)
	}
	if !isLowercaseHex(request.PasswordHash[:]) {
		return Request{}, ErrMalformedPasswordHash
	}
	if err := readCRLF(reader); err != nil {
		return Request{}, err
	}

	var command [1]byte
	if _, err := io.ReadFull(reader, command[:]); err != nil {
		return Request{}, fmt.Errorf("trojan: reading command: %w", err)
	}
	if command[0] != CommandConnect && command[0] != CommandUDPAssociate {
		return Request{}, fmt.Errorf("%w: X'%02X'", ErrUnknownCommand, command[0])
	}
	request.Command = command[0]

	destination, err := socksaddr.ReadAddress(reader)
	if err != nil {
		return Request{}, err
	}
	request.Destination = destination

	if err := readCRLF(reader); err != nil {
		return Request{}, err
	}
	return request, nil
}

// RequestLength reports how long the header beginning at prefix is, reading
// only as far as it must. io.ErrUnexpectedEOF means prefix is too short to
// tell yet. A caller peeking a stream grows the prefix until this succeeds.
func RequestLength(prefix []byte) (int, error) {
	const fixed = PasswordHashLength + len(crlf) + 1
	if len(prefix) < fixed+1 {
		return 0, io.ErrUnexpectedEOF
	}
	addressLength, err := encodedAddressLength(prefix[fixed:])
	if err != nil {
		return 0, err
	}
	return fixed + addressLength + len(crlf), nil
}

// AppendUDPFrame encodes one datagram. A payload longer than the length field
// can express is spec.ErrMessageTooLong, not a truncation.
func AppendUDPFrame(destination []byte, address spec.Destination, payload []byte) ([]byte, error) {
	if len(payload) > MaxUDPPayload {
		return nil, fmt.Errorf("trojan: %d byte datagram: %w", len(payload), spec.ErrMessageTooLong)
	}
	destination, err := socksaddr.AppendAddress(destination, address)
	if err != nil {
		return nil, err
	}
	destination = binary.BigEndian.AppendUint16(destination, uint16(len(payload)))
	destination = append(destination, crlf[:]...)
	return append(destination, payload...), nil
}

// ReadUDPFrame reads one frame into payload. A frame larger than payload is
// read to its end and dropped, so the stream stays aligned on the next frame,
// and reported as spec.ErrMessageTooLong.
func ReadUDPFrame(reader io.Reader, payload []byte) (int, spec.Destination, error) {
	address, err := socksaddr.ReadAddress(reader)
	if err != nil {
		return 0, spec.Destination{}, err
	}

	var length [2]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return 0, spec.Destination{}, fmt.Errorf("trojan: reading datagram length: %w", err)
	}
	size := int(binary.BigEndian.Uint16(length[:]))

	if err := readCRLF(reader); err != nil {
		return 0, spec.Destination{}, err
	}

	if size > len(payload) {
		if _, err := io.CopyN(io.Discard, reader, int64(size)); err != nil {
			return 0, spec.Destination{}, fmt.Errorf("trojan: skipping datagram: %w", err)
		}
		return 0, address, fmt.Errorf("trojan: %d byte datagram into a %d byte buffer: %w", size, len(payload), spec.ErrMessageTooLong)
	}
	if _, err := io.ReadFull(reader, payload[:size]); err != nil {
		return 0, spec.Destination{}, fmt.Errorf("trojan: reading datagram: %w", err)
	}
	return size, address, nil
}

func readCRLF(reader io.Reader) error {
	var got [2]byte
	if _, err := io.ReadFull(reader, got[:]); err != nil {
		return fmt.Errorf("trojan: reading CRLF: %w", err)
	}
	if got != crlf {
		return ErrMissingCRLF
	}
	return nil
}

func encodedAddressLength(prefix []byte) (int, error) {
	switch prefix[0] {
	case socksaddr.AddressIPv4:
		return 1 + 4 + 2, nil
	case socksaddr.AddressIPv6:
		return 1 + 16 + 2, nil
	case socksaddr.AddressDomain:
		if len(prefix) < 2 {
			return 0, io.ErrUnexpectedEOF
		}
		if prefix[1] == 0 {
			return 0, socksaddr.ErrEmptyDomain
		}
		return 1 + 1 + int(prefix[1]) + 2, nil
	default:
		return 0, fmt.Errorf("%w: X'%02X'", socksaddr.ErrUnsupportedAddressType, prefix[0])
	}
}

func isLowercaseHex(text []byte) bool {
	for _, character := range text {
		if !('0' <= character && character <= '9' || 'a' <= character && character <= 'f') {
			return false
		}
	}
	return true
}
