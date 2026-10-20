// Package handshake is the HTTP/1.1 WebSocket upgrade handshake that the ws
// and httpupgrade transports both start with: a GET asking to switch to
// websocket, and a 101 answer. It reads and bounds the handshake, answers
// refusals as an ordinary web server would, and writes the request headers the
package handshake

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/we-apranik/apranik/spec"
)

const MaxHandshakeLength = 64 << 10

const ReaderSize = 4096

// Without its own User-Agent a client request stands out; Xray also sends a
// browser's by default. The Chrome version matches the ClientHello of the tls
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

var (
	ErrHandshakeTooLarge = errors.New("handshake larger than 64 KiB")
	ErrPathWithoutSlash  = errors.New(`path must start with "/"`)
)

type LimitedReader struct {
	underlyingStream spec.Stream
	remaining        int
	limited          bool
}

func NewReader(underlyingStream spec.Stream) (*LimitedReader, *bufio.Reader) {
	limiter := &LimitedReader{underlyingStream: underlyingStream, remaining: MaxHandshakeLength, limited: true}
	return limiter, bufio.NewReaderSize(limiter, ReaderSize)
}

func (r *LimitedReader) Read(p []byte) (int, error) {
	if !r.limited {
		return r.underlyingStream.Read(p)
	}
	if r.remaining <= 0 {
		return 0, ErrHandshakeTooLarge
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.underlyingStream.Read(p)
	r.remaining -= n
	return n, err
}

func (r *LimitedReader) LiftLimit() { r.limited = false }

func WriteRefusal(writer io.Writer, status int) {
	fmt.Fprintf(writer, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", status, http.StatusText(status))
}

func HeaderContainsToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, element := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(element), token) {
				return true
			}
		}
	}
	return false
}

// HostMatches compares a request's Host with the one a server requires,
// ignoring case and any port on either, as Xray does.
func HostMatches(requestHost, requiredHost string) bool {
	return strings.EqualFold(hostWithoutPort(requestHost), hostWithoutPort(requiredHost))
}

func hostWithoutPort(host string) string {
	if withoutPort, _, err := net.SplitHostPort(host); err == nil {
		return withoutPort
	}
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

func HostHeaderFor(server spec.Destination) string {
	if server.Port() == 80 || server.Port() == 443 {
		if ip, isIP := server.IP(); isIP && ip.Is6() {
			return "[" + server.Host() + "]"
		}
		return server.Host()
	}
	return server.String()
}

func RequestHeaderLines(headers map[string]string) string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines strings.Builder
	hasUserAgent := false
	for _, name := range names {
		fmt.Fprintf(&lines, "%s: %s\r\n", name, headers[name])
		hasUserAgent = hasUserAgent || strings.EqualFold(name, "User-Agent")
	}
	if !hasUserAgent {
		fmt.Fprintf(&lines, "User-Agent: %s\r\n", DefaultUserAgent)
	}
	return lines.String()
}

func ValidatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return ErrPathWithoutSlash
	}
	if strings.ContainsAny(path, " \r\n") {
		return fmt.Errorf("path %q contains a space or a line break", path)
	}
	return nil
}

func ValidateHeaders(headers map[string]string, namesTheTransportOwns []string) error {
	for name, value := range headers {
		if !IsHeaderName(name) {
			return fmt.Errorf("%q is not a valid header name", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("the value of %q contains a line break", name)
		}
		for _, owned := range namesTheTransportOwns {
			if strings.EqualFold(name, owned) {
				return fmt.Errorf("%q is written by the transport itself", name)
			}
		}
	}
	return nil
}

func ValidateHost(host string) error {
	if strings.ContainsAny(host, " /\r\n") {
		return fmt.Errorf("host %q contains a space, a slash or a line break", host)
	}
	return nil
}

func IsHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range []byte(name) {
		switch {
		case 'a' <= character && character <= 'z', 'A' <= character && character <= 'Z', '0' <= character && character <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", character) >= 0:
		default:
			return false
		}
	}
	return true
}
