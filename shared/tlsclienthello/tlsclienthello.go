// Package tlsclienthello reads the one TLS message sent in plaintext, the
// ClientHello: joins it from its records and finds the server name (SNI) in
// it. The tls sniffer reports that name; the fragment transport cuts inside it.
package tlsclienthello

import (
	"errors"

	"github.com/The-Apranik/Apranik/spec"
)

const (
	RecordTypeHandshake = 0x16
	RecordHeaderSize    = 5

	// The largest body a record may carry: 2^14 of plaintext plus the
	// allowance RFC 8446 section 5.2 gives a protected record.
	MaxRecordBodySize = 1<<14 + 256

	HandshakeHeaderSize = 4

	handshakeTypeClientHello = 0x01
	extensionServerName      = 0x0000
	serverNameTypeHostName   = 0x00
	maxClientHelloSize       = 1 << 16
)

var (
	ErrNotClientHello = errors.New("tlsclienthello: not a TLS ClientHello")
	ErrMalformed      = errors.New("tlsclienthello: malformed ClientHello")
)

// IsHandshakeRecordHeader reports whether header, at least RecordHeaderSize
// bytes, starts a TLS handshake record of a plausible size.
func IsHandshakeRecordHeader(header []byte) bool {
	if len(header) < RecordHeaderSize {
		return false
	}
	bodySize := RecordBodySize(header)
	return header[0] == RecordTypeHandshake && header[1] == 3 && header[2] <= 4 &&
		bodySize > 0 && bodySize <= MaxRecordBodySize
}

// RecordBodySize reads the length field of a record header.
func RecordBodySize(header []byte) int {
	return int(header[3])<<8 | int(header[4])
}

// Reassemble joins the handshake bytes of the records at the start of data
// until the whole ClientHello is present, and returns its body: the message
// without its 4-byte handshake header. spec.ErrSniffNeedMoreData means data
// is a valid start that ends too soon.
func Reassemble(data []byte) ([]byte, error) {
	var handshake []byte
	for {
		if len(data) < RecordHeaderSize {
			if len(data) > 0 && data[0] != RecordTypeHandshake {
				return nil, ErrNotClientHello
			}
			return nil, spec.ErrSniffNeedMoreData
		}
		if !IsHandshakeRecordHeader(data) {
			return nil, ErrNotClientHello
		}
		bodySize := RecordBodySize(data)
		if len(data) < RecordHeaderSize+bodySize {
			return nil, spec.ErrSniffNeedMoreData
		}
		handshake = append(handshake, data[RecordHeaderSize:RecordHeaderSize+bodySize]...)
		data = data[RecordHeaderSize+bodySize:]

		if len(handshake) < HandshakeHeaderSize {
			continue
		}
		if handshake[0] != handshakeTypeClientHello {
			return nil, ErrNotClientHello
		}
		messageSize := HandshakeHeaderSize + (int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3]))
		if messageSize > maxClientHelloSize {
			return nil, ErrMalformed
		}
		if len(handshake) >= messageSize {
			return handshake[HandshakeHeaderSize:messageSize], nil
		}
	}
}

// ServerName is the host name a ClientHello asks for, and where it sits.
type ServerName struct {
	Name string

	// Byte offset of the name within the ClientHello body given to
	// FindServerName.
	Offset int
}

// FindServerName returns the zero ServerName, and no error, for a
// ClientHello that names no server. body is what Reassemble returns.
func FindServerName(body []byte) (ServerName, error) {
	reader := byteReader{remaining: body}
	reader.skip(2 + 32)    // legacy_version, random
	reader.skipVariable(1) // legacy_session_id
	reader.skipVariable(2) // cipher_suites
	reader.skipVariable(1) // legacy_compression_methods
	if reader.failed {
		return ServerName{}, ErrMalformed
	}
	if len(reader.remaining) == 0 {
		return ServerName{}, nil
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
				// Every slice shares body's backing array, so the capacity
				// left after name's start gives its offset.
				return ServerName{Name: string(name), Offset: cap(body) - cap(name)}, nil
			}
		}
		if serverNameList.failed || names.failed {
			return ServerName{}, ErrMalformed
		}
	}
	if reader.failed || extensions.failed {
		return ServerName{}, ErrMalformed
	}
	return ServerName{}, nil
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
