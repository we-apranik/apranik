// Package tls is a sniffer: it recognises a TLS ClientHello in the first
// bytes of a stream and reports the server name (SNI) the client asked for,
// so routing can see a domain where the client only gave an IP.
package tls

import (
	"errors"
	"strings"

	"github.com/The-Apranik/Apranik/spec"
)

const (
	recordTypeHandshake      = 0x16
	handshakeTypeClientHello = 0x01
	extensionServerName      = 0x0000
	serverNameTypeHostName   = 0x00

	recordHeaderSize    = 5
	handshakeHeaderSize = 4
	maxRecordBodySize   = 1<<14 + 256
	maxClientHelloSize  = 1 << 16
)

var (
	errNotTLS               = errors.New("tls: not a TLS ClientHello")
	errMalformedClientHello = errors.New("tls: malformed ClientHello")
)

type Settings struct{}

func (*Settings) Validate() error    { return nil }
func (*Settings) Warnings() []string { return nil }

type Sniffer struct{ Settings }

func (*Sniffer) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "tls", Description: "sniff server name (SNI) from a TLS ClientHello"}
}

func (*Sniffer) Sniff(payloadPrefix []byte) (spec.SniffResult, error) {
	clientHello, err := reassembleClientHello(payloadPrefix)
	if err != nil {
		return spec.SniffResult{}, err
	}
	serverName, err := serverNameFromClientHello(clientHello)
	if err != nil {
		return spec.SniffResult{}, err
	}
	return spec.SniffResult{Protocol: "tls", Domain: strings.TrimSpace(serverName)}, nil
}

func NewSnifferModule() spec.Sniffer { return &Sniffer{} }

// A ClientHello may be split across several records; the handshake bytes of
// each are joined until the whole message is present.
func reassembleClientHello(data []byte) ([]byte, error) {
	var handshake []byte
	for {
		if len(data) < recordHeaderSize {
			if len(data) > 0 && data[0] != recordTypeHandshake {
				return nil, errNotTLS
			}
			return nil, spec.ErrSniffNeedMoreData
		}
		if data[0] != recordTypeHandshake || data[1] != 3 || data[2] > 4 {
			return nil, errNotTLS
		}
		bodySize := int(data[3])<<8 | int(data[4])
		if bodySize == 0 || bodySize > maxRecordBodySize {
			return nil, errNotTLS
		}
		if len(data) < recordHeaderSize+bodySize {
			return nil, spec.ErrSniffNeedMoreData
		}
		handshake = append(handshake, data[recordHeaderSize:recordHeaderSize+bodySize]...)
		data = data[recordHeaderSize+bodySize:]

		if len(handshake) < handshakeHeaderSize {
			continue
		}
		if handshake[0] != handshakeTypeClientHello {
			return nil, errNotTLS
		}
		messageSize := handshakeHeaderSize + (int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3]))
		if messageSize > maxClientHelloSize {
			return nil, errMalformedClientHello
		}
		if len(handshake) >= messageSize {
			return handshake[handshakeHeaderSize:messageSize], nil
		}
	}
}

// Returns "" with no error for a ClientHello that names no server.
func serverNameFromClientHello(body []byte) (string, error) {
	reader := byteReader{remaining: body}
	reader.skip(2 + 32)    // legacy_version, random
	reader.skipVariable(1) // legacy_session_id
	reader.skipVariable(2) // cipher_suites
	reader.skipVariable(1) // legacy_compression_methods
	if reader.failed {
		return "", errMalformedClientHello
	}
	if len(reader.remaining) == 0 {
		return "", nil
	}

	extensions := byteReader{remaining: reader.variable(2)}
	for len(extensions.remaining) > 0 && !extensions.failed {
		extensionType := extensions.uint16()
		extensionData := extensions.variable(2)
		if extensionType != extensionServerName {
			continue
		}

		serverNameList := byteReader{remaining: extensionData}
		names := byteReader{remaining: serverNameList.variable(2)}
		for len(names.remaining) > 0 && !names.failed {
			nameType := names.uint8()
			name := names.variable(2)
			if nameType == serverNameTypeHostName && !names.failed {
				return string(name), nil
			}
		}
		if serverNameList.failed || names.failed {
			return "", errMalformedClientHello
		}
	}
	if reader.failed || extensions.failed {
		return "", errMalformedClientHello
	}
	return "", nil
}

type byteReader struct {
	remaining []byte
	failed    bool
}

func (r *byteReader) take(size int) []byte {
	if r.failed || size > len(r.remaining) {
		r.failed = true
		return nil
	}
	taken := r.remaining[:size]
	r.remaining = r.remaining[size:]
	return taken
}

func (r *byteReader) skip(size int) { r.take(size) }

func (r *byteReader) uint8() int {
	taken := r.take(1)
	if taken == nil {
		return 0
	}
	return int(taken[0])
}

func (r *byteReader) uint16() int {
	taken := r.take(2)
	if taken == nil {
		return 0
	}
	return int(taken[0])<<8 | int(taken[1])
}

func (r *byteReader) variable(lengthSize int) []byte {
	var size int
	if lengthSize == 1 {
		size = r.uint8()
	} else {
		size = r.uint16()
	}
	return r.take(size)
}

func (r *byteReader) skipVariable(lengthSize int) { r.variable(lengthSize) }
