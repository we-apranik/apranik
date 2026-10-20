// Package tlsclienthello reads the one TLS message sent in plaintext, the
// ClientHello: joins it from its records, finds the server name (SNI) in it
// and lists its extensions. The tls sniffer reports the name and guesses the
// browser from the extensions; the fragment transport cuts inside the name.
package tlsclienthello

import (
	"errors"

	"github.com/we-apranik/apranik/spec"
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

func IsHandshakeRecordHeader(header []byte) bool {
	if len(header) < RecordHeaderSize {
		return false
	}
	bodySize := RecordBodySize(header)
	return header[0] == RecordTypeHandshake && header[1] == 3 && header[2] <= 4 &&
		bodySize > 0 && bodySize <= MaxRecordBodySize
}

func RecordBodySize(header []byte) int {
	return int(header[3])<<8 | int(header[4])
}

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

type ServerName struct {
	Name string

	Offset int
}

func FindServerName(body []byte) (ServerName, error) {
	var serverName ServerName
	var nameErr error
	err := forEachExtension(body, func(extensionType int, extensionData []byte) bool {
		if extensionType != extensionServerName {
			return true
		}
		serverNameList := byteReader{remaining: extensionData}
		names := byteReader{remaining: serverNameList.variable(2)}
		for len(names.remaining) > 0 && !names.failed {
			nameType := names.uint8()
			name := names.variable(2)
			if nameType == serverNameTypeHostName && !names.failed {
				serverName = ServerName{Name: string(name), Offset: cap(body) - cap(name)}
				return false
			}
		}
		if serverNameList.failed || names.failed {
			nameErr = ErrMalformed
			return false
		}
		return true
	})
	if err != nil {
		return ServerName{}, err
	}
	if nameErr != nil {
		return ServerName{}, nameErr
	}
	return serverName, nil
}

func ExtensionTypes(body []byte) ([]uint16, error) {
	var extensionTypes []uint16
	err := forEachExtension(body, func(extensionType int, _ []byte) bool {
		extensionTypes = append(extensionTypes, uint16(extensionType))
		return true
	})
	if err != nil {
		return nil, err
	}
	return extensionTypes, nil
}

// IsGREASE reports whether value is one of RFC 8701's reserved GREASE values
// (0x0a0a, 0x1a1a, ... 0xfafa), which Chromium and Apple clients scatter in
func IsGREASE(value uint16) bool {
	return value&0x0f0f == 0x0a0a && value>>8 == value&0xff
}

func forEachExtension(body []byte, visit func(extensionType int, extensionData []byte) bool) error {
	reader := byteReader{remaining: body}
	reader.skip(2 + 32)
	reader.skipVariable(1)
	reader.skipVariable(2)
	reader.skipVariable(1)
	if reader.failed {
		return ErrMalformed
	}
	if len(reader.remaining) == 0 {
		return nil
	}

	extensions := byteReader{remaining: reader.variable(2)}
	for len(extensions.remaining) > 0 && !extensions.failed {
		extensionType := extensions.uint16()
		extensionData := extensions.variable(2)
		if extensions.failed {
			break
		}
		if !visit(extensionType, extensionData) {
			return nil
		}
	}
	if reader.failed || extensions.failed {
		return ErrMalformed
	}
	return nil
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
