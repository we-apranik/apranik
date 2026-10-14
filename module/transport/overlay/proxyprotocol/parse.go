package proxyprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"

	"github.com/we-apranik/apranik/spec"
)

// The first twelve bytes tell the versions apart, and no valid header is
// shorter than that: the shortest version 1 line, "PROXY UNKNOWN\r\n", is
// fifteen. So reading twelve never reads past the header.
const signatureLength = 12

var versionTwoSignature = []byte("\r\n\r\n\x00\r\nQUIT\n")

const (
	versionOneMaxLength = 107 // the specification's limit, line ending included

	// The header also carries TLV extensions, which are read and skipped. No
	// sender needs room for more than a few hundred bytes. PROVISIONAL.
	versionTwoMaxLength = 4096
)

var (
	errNoHeader  = errors.New("the first bytes are not a PROXY protocol header")
	errMalformed = errors.New("the PROXY protocol header is malformed")
)

// header holds what the overlay uses. hasClient is false for a header that
// names no client (LOCAL, UNKNOWN, UNSPEC, unix or datagram addresses): the
// connection keeps its peer.
type header struct {
	version     int
	hasClient   bool
	source      spec.Endpoint
	destination spec.Endpoint
}

// readHeader reads exactly the header and not a byte more, so the stream it
// came from can be handed on untouched.
func readHeader(stream io.Reader) (header, error) {
	signature := make([]byte, signatureLength)
	if _, err := io.ReadFull(stream, signature); err != nil {
		return header{}, fmt.Errorf("reading the first %d bytes: %w", signatureLength, err)
	}
	switch {
	case bytes.Equal(signature, versionTwoSignature):
		return readVersionTwo(stream)
	case bytes.HasPrefix(signature, []byte("PROXY ")):
		return readVersionOne(stream, signature)
	default:
		return header{}, errNoHeader
	}
}

func readVersionOne(stream io.Reader, line []byte) (header, error) {
	if bytes.Contains(line, []byte("\r\n")) {
		return header{}, fmt.Errorf("%w: the line ends too early", errMalformed)
	}
	next := make([]byte, 1)
	for !bytes.HasSuffix(line, []byte("\r\n")) {
		if len(line) >= versionOneMaxLength {
			return header{}, fmt.Errorf("%w: the line is longer than %d bytes", errMalformed, versionOneMaxLength)
		}
		if _, err := io.ReadFull(stream, next); err != nil {
			return header{}, fmt.Errorf("reading the header line: %w", err)
		}
		line = append(line, next[0])
	}
	return parseVersionOne(strings.TrimSuffix(string(line), "\r\n"))
}

// "PROXY TCP4 192.0.2.1 198.51.100.1 56324 443", or "PROXY UNKNOWN" followed by
// anything.
func parseVersionOne(line string) (header, error) {
	fields := strings.Split(line, " ")
	if len(fields) >= 2 && fields[1] == "UNKNOWN" {
		return header{version: 1}, nil
	}
	if len(fields) != 6 || (fields[1] != "TCP4" && fields[1] != "TCP6") {
		return header{}, fmt.Errorf("%w: %q", errMalformed, line)
	}
	sourceAddress, sourceErr := netip.ParseAddr(fields[2])
	destinationAddress, destinationErr := netip.ParseAddr(fields[3])
	sourcePort, sourcePortErr := strconv.ParseUint(fields[4], 10, 16)
	destinationPort, destinationPortErr := strconv.ParseUint(fields[5], 10, 16)
	if err := errors.Join(sourceErr, destinationErr, sourcePortErr, destinationPortErr); err != nil {
		return header{}, fmt.Errorf("%w: %q", errMalformed, line)
	}
	wantFourBytes := fields[1] == "TCP4"
	if sourceAddress.Is4() != wantFourBytes || destinationAddress.Is4() != wantFourBytes {
		return header{}, fmt.Errorf("%w: the addresses are not %s addresses", errMalformed, fields[1])
	}
	return clientHeader(1, sourceAddress, uint16(sourcePort), destinationAddress, uint16(destinationPort))
}

const (
	versionTwoCommandLocal = 0x0
	versionTwoCommandProxy = 0x1

	versionTwoFamilyInet  = 0x1
	versionTwoFamilyInet6 = 0x2

	versionTwoProtocolStream = 0x1
)

func readVersionTwo(stream io.Reader) (header, error) {
	fixed := make([]byte, 4)
	if _, err := io.ReadFull(stream, fixed); err != nil {
		return header{}, fmt.Errorf("reading the version 2 header: %w", err)
	}
	if fixed[0]>>4 != 2 {
		return header{}, fmt.Errorf("%w: version %d is not 2", errMalformed, fixed[0]>>4)
	}
	command, family, protocol := fixed[0]&0x0f, fixed[1]>>4, fixed[1]&0x0f
	length := int(binary.BigEndian.Uint16(fixed[2:]))
	if length > versionTwoMaxLength {
		return header{}, fmt.Errorf("%w: %d bytes of addresses and extensions, more than the %d allowed", errMalformed, length, versionTwoMaxLength)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(stream, body); err != nil {
		return header{}, fmt.Errorf("reading the version 2 addresses: %w", err)
	}

	switch command {
	case versionTwoCommandLocal:
		return header{version: 2}, nil
	case versionTwoCommandProxy:
	default:
		return header{}, fmt.Errorf("%w: command %d is neither LOCAL nor PROXY", errMalformed, command)
	}
	if protocol != versionTwoProtocolStream {
		return header{version: 2}, nil
	}
	switch family {
	case versionTwoFamilyInet:
		if len(body) < 12 {
			return header{}, fmt.Errorf("%w: %d bytes cannot hold two IPv4 addresses and ports", errMalformed, len(body))
		}
		source, destination := netip.AddrFrom4([4]byte(body[0:4])), netip.AddrFrom4([4]byte(body[4:8]))
		return clientHeader(2, source, binary.BigEndian.Uint16(body[8:10]), destination, binary.BigEndian.Uint16(body[10:12]))
	case versionTwoFamilyInet6:
		if len(body) < 36 {
			return header{}, fmt.Errorf("%w: %d bytes cannot hold two IPv6 addresses and ports", errMalformed, len(body))
		}
		source, destination := netip.AddrFrom16([16]byte(body[0:16])), netip.AddrFrom16([16]byte(body[16:32]))
		return clientHeader(2, source, binary.BigEndian.Uint16(body[32:34]), destination, binary.BigEndian.Uint16(body[34:36]))
	default:
		return header{version: 2}, nil
	}
}

func clientHeader(version int, sourceAddress netip.Addr, sourcePort uint16, destinationAddress netip.Addr, destinationPort uint16) (header, error) {
	source, sourceErr := spec.NewEndpoint(sourceAddress.Unmap(), sourcePort)
	destination, destinationErr := spec.NewEndpoint(destinationAddress.Unmap(), destinationPort)
	if err := errors.Join(sourceErr, destinationErr); err != nil {
		return header{}, fmt.Errorf("%w: %v", errMalformed, err)
	}
	return header{version: version, hasClient: true, source: source, destination: destination}, nil
}
