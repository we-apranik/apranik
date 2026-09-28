// Package handshake is the HTTP/1.1 WebSocket upgrade handshake that the ws
// and httpupgrade transports both start with: a GET asking to switch to
// websocket, and a 101 answer. It reads and bounds the handshake, answers
// refusals as an ordinary web server would, and writes the request headers the
// way Xray and sing-box clients do. It knows nothing of what follows the 101.
package handshake

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// Room for a handshake carrying a WebSocket's largest early data, and no
// more: a peer that sends an endless header is cut off here, not by memory.
const MaxHandshakeLength = 64 << 10

// Big enough for a whole handshake in one read. The same reader goes on to
// hold whatever follows the handshake, so bytes read past it are not lost.
const ReaderSize = 4096

// Without its own User-Agent a client request stands out; Xray also sends a
// browser's by default. PROVISIONAL: a fixed string that will age.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

var (
	ErrHandshakeTooLarge = errors.New("handshake larger than 64 KiB")
	ErrPathWithoutSlash  = errors.New(`path must start with "/"`)
)

// LimitedReader counts what the handshake reads and refuses more than
// MaxHandshakeLength. LiftLimit turns it into a plain pass-through once the
// handshake is done.
type LimitedReader struct {
	underlyingStream spec.Stream
	remaining        int
	limited          bool
}

// NewReader returns the limiter and the buffered reader over it that the
// handshake, and then the stream, read through.
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

// BoundByContext makes blocking reads and writes on the stream fail once ctx
// ends. release undoes it and must be called before the stream is handed on;
// it fails if ctx ended first, since the stream's deadline is then already
// spent and the handshake must be abandoned.
func BoundByContext(ctx context.Context, underlyingStream spec.Stream) (release func() error) {
	if deadline, ok := ctx.Deadline(); ok {
		underlyingStream.SetDeadline(deadline)
	}
	stopInterrupting := context.AfterFunc(ctx, func() {
		underlyingStream.SetDeadline(time.Unix(1, 0))
	})
	return func() error {
		if !stopInterrupting() {
			return ctx.Err()
		}
		underlyingStream.SetDeadline(time.Time{})
		return nil
	}
}

// WriteRefusal answers the way an ordinary web server would, so a prober
// that gets the host, path or upgrade wrong learns nothing more.
func WriteRefusal(writer io.Writer, status int) {
	fmt.Fprintf(writer, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", status, http.StatusText(status))
}

// HeaderContainsToken reports whether a comma-separated header such as
// Connection lists token, ignoring case, so "keep-alive, Upgrade" counts.
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
// ignoring case and any port, as Xray does.
func HostMatches(requestHost, requiredHost string) bool {
	if host, _, err := net.SplitHostPort(requestHost); err == nil {
		requestHost = host
	}
	requestHost = strings.TrimSuffix(strings.TrimPrefix(requestHost, "["), "]")
	requiredHost = strings.TrimSuffix(strings.TrimPrefix(requiredHost, "["), "]")
	return strings.EqualFold(requestHost, requiredHost)
}

// HostHeaderFor is the Host a client sends when none is configured: the
// server's address, with the port only when it is not HTTP's default.
func HostHeaderFor(server spec.Destination) string {
	if server.Port() == 80 || server.Port() == 443 {
		if ip, isIP := server.IP(); isIP && ip.Is6() {
			return "[" + server.Host() + "]"
		}
		return server.Host()
	}
	return server.String()
}

// RequestHeaderLines renders extra headers, sorted so every request from one
// outbound has the same order, plus DefaultUserAgent when none is given.
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

// ValidatePath checks a configured request path.
func ValidatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return ErrPathWithoutSlash
	}
	if strings.ContainsAny(path, " \r\n") {
		return fmt.Errorf("path %q contains a space or a line break", path)
	}
	return nil
}

// ValidateHeaders checks configured extra headers: valid names, values
// without line breaks, and none of the names the transport writes itself.
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

// ValidateHost checks a configured Host: a name or address, with no
// characters that would break the request.
func ValidateHost(host string) error {
	if strings.ContainsAny(host, " /\r\n") {
		return fmt.Errorf("host %q contains a space, a slash or a line break", host)
	}
	return nil
}

// IsHeaderName reports whether name is an RFC 9110 token.
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
