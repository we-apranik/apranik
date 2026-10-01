// Package wire is the HTTP proxy byte format: reading a request head without
// consuming payload, Basic credentials, and request address forms. It is kept
// apart from the protocol so it can be tested byte for byte.
package wire

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"

	"github.com/The-Apranik/Apranik/spec"
)

const (
	// HeadReaderSize is the reader size used for a head. Anything past the head is
	// payload and must survive, which is what ReplayStream is for.
	HeadReaderSize = 8 << 10

	maxMessageHeadBytes = 64 << 10
	maxHeaderLines      = 128
)

// Every connection reads its head through one of these and ReplayStream copies
// out whatever is left, so the reader can serve the next connection instead of
// becoming garbage.
var headReaders = sync.Pool{New: func() any { return bufio.NewReaderSize(nil, HeadReaderSize) }}

// AcquireHeadReader returns a pooled reader over underlyingReader. The caller must
// ReleaseHeadReader it once the head is read and ReplayStream has been called.
func AcquireHeadReader(underlyingReader io.Reader) *bufio.Reader {
	reader := headReaders.Get().(*bufio.Reader)
	reader.Reset(underlyingReader)
	return reader
}

// ReleaseHeadReader drops the reader's underlying reader and any bytes it still holds,
// so a pooled reader never keeps a connection alive.
func ReleaseHeadReader(reader *bufio.Reader) {
	reader.Reset(nil)
	headReaders.Put(reader)
}

type ConnectRefusedError struct {
	StatusCode int
	StatusLine string
}

func (e *ConnectRefusedError) Error() string {
	return fmt.Sprintf("http: proxy answered CONNECT with %q", e.StatusLine)
}

// replayingStream replays bytes already read off the underlying stream before handing over
// the rest. Both halves need it: clients pipeline payload straight after
// CONNECT, and the reader used for the head will have swallowed it.
type replayingStream struct {
	spec.Stream
	reader io.Reader
}

func (s *replayingStream) Read(buffer []byte) (int, error) { return s.reader.Read(buffer) }

// ReplayStream returns a stream that yields replayedHead, then whatever the reader
// buffered, then the underlying stream itself. The buffered bytes are copied, so the
// reader can be released and reused.
func ReplayStream(underlyingStream spec.Stream, replayedHead []byte, buffered *bufio.Reader) spec.Stream {
	var readers []io.Reader
	if len(replayedHead) > 0 {
		readers = append(readers, bytes.NewReader(replayedHead))
	}
	if pending := buffered.Buffered(); pending > 0 {
		leftover, _ := buffered.Peek(pending)
		readers = append(readers, bytes.NewReader(bytes.Clone(leftover)))
	}
	if len(readers) == 0 {
		return underlyingStream
	}
	return &replayingStream{Stream: underlyingStream, reader: io.MultiReader(append(readers, underlyingStream)...)}
}

func ReadMessageHead(reader *bufio.Reader) (string, []string, error) {
	size := 0

	first, err := readHeadLine(reader, &size)
	if err != nil {
		return "", nil, err
	}

	var headers []string
	for {
		line, err := readHeadLine(reader, &size)
		if err != nil {
			return "", nil, err
		}
		if line == "" {
			return first, headers, nil
		}
		if !strings.Contains(line, ":") {
			return "", nil, fmt.Errorf("http: malformed header line %q", line)
		}
		if len(headers) == maxHeaderLines {
			return "", nil, fmt.Errorf("http: more than %d header lines", maxHeaderLines)
		}
		headers = append(headers, line)
	}
}

func readHeadLine(reader *bufio.Reader, size *int) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("http: reading head: %w", err)
	}
	if *size += len(line); *size > maxMessageHeadBytes {
		return "", fmt.Errorf("http: head longer than %d bytes", maxMessageHeadBytes)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func FindHeaderValue(headers []string, name string) string {
	for _, header := range headers {
		colon := strings.IndexByte(header, ':')
		if colon > 0 && strings.EqualFold(strings.TrimSpace(header[:colon]), name) {
			return strings.TrimSpace(header[colon+1:])
		}
	}
	return ""
}

func HeaderFieldName(header string) string {
	if colon := strings.IndexByte(header, ':'); colon > 0 {
		return strings.TrimSpace(header[:colon])
	}
	return ""
}

func BasicAuthorizationValue(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// AuthorizationValuesMatch compares in constant time.
func AuthorizationValuesMatch(offered, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(offered), []byte(expected)) == 1
}

// ParseHostPort keeps a domain unresolved: only the base transport resolves.
func ParseHostPort(hostPort string, defaultPort uint16) (spec.Destination, error) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		if defaultPort == 0 {
			return spec.Destination{}, fmt.Errorf("http: %q has no port: %w", hostPort, err)
		}
		host, port = hostPort, strconv.Itoa(int(defaultPort))
	}
	if host == "" {
		return spec.Destination{}, fmt.Errorf("http: %q has no host", hostPort)
	}

	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return spec.Destination{}, fmt.Errorf("http: %q has no usable port", hostPort)
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		return spec.NewDestinationFromIP(ip, uint16(number))
	}
	return spec.NewDestinationFromDomain(host, uint16(number))
}

func FormatHostPort(address spec.Destination) (string, error) {
	switch {
	case !address.IsValid():
		return "", fmt.Errorf("http: address has neither domain nor IP")
	case address.Port() == 0:
		return "", fmt.Errorf("http: address has no port")
	default:
		return address.String(), nil
	}
}
