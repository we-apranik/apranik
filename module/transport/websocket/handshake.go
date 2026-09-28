package websocket

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// RFC 6455 section 1.3.
const acceptKeySuffix = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Room for a handshake carrying the largest early data, and no more: a peer
// that sends an endless header is cut off here, not by memory.
const maxHandshakeLength = 64 << 10

// Big enough for a whole handshake in one read; after it, the same reader
// holds frames, so bytes already read past the handshake are not lost.
const readerSize = 4096

// Without its own User-Agent a client request stands out; Xray also sends a
// browser's by default. PROVISIONAL: a fixed string that will age.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

var (
	errNotAnUpgrade      = errors.New("websocket: not a WebSocket upgrade request")
	errWrongPath         = errors.New("websocket: wrong path")
	errUpgradeRefused    = errors.New("websocket: the server did not switch protocols")
	errWrongAcceptKey    = errors.New("websocket: the server's Sec-WebSocket-Accept does not match the key")
	errBadEarlyData      = errors.New("websocket: early data is not valid base64")
	errHandshakeTooLarge = errors.New("websocket: handshake larger than 64 KiB")
)

// An endpoint's key and the value the peer must answer with.
func acceptKeyFor(key string) string {
	digest := sha1.Sum([]byte(key + acceptKeySuffix))
	return base64.StdEncoding.EncodeToString(digest[:])
}

func newKey() string {
	var nonce [16]byte
	rand.Read(nonce[:])
	return base64.StdEncoding.EncodeToString(nonce[:])
}

// Xray tolerates standard base64 by mapping it to URL-safe; so does this.
var standardToURLSafeBase64 = strings.NewReplacer("+", "-", "/", "_", "=", "")

func decodeEarlyData(text string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(standardToURLSafeBase64.Replace(text))
}

func encodeEarlyData(payload []byte) string {
	return base64.RawURLEncoding.EncodeToString(payload)
}

// handshakeReader counts what the handshake reads and refuses more than
// maxHandshakeLength. Once the handshake is done, the limit is lifted and it
// passes frames through untouched.
type handshakeReader struct {
	underlyingStream spec.Stream
	remaining        int
	limited          bool
}

func (r *handshakeReader) Read(p []byte) (int, error) {
	if !r.limited {
		return r.underlyingStream.Read(p)
	}
	if r.remaining <= 0 {
		return 0, errHandshakeTooLarge
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.underlyingStream.Read(p)
	r.remaining -= n
	return n, err
}

func newHandshakeReader(underlyingStream spec.Stream) (*handshakeReader, *bufio.Reader) {
	limiter := &handshakeReader{underlyingStream: underlyingStream, remaining: maxHandshakeLength, limited: true}
	return limiter, bufio.NewReaderSize(limiter, readerSize)
}

// boundByContext makes blocking reads and writes on the stream fail once ctx
// ends. release undoes it and must be called before the stream is handed on;
// it fails if ctx ended first, since the stream's deadline is then already
// spent and the handshake must be abandoned.
func boundByContext(ctx context.Context, underlyingStream spec.Stream) (release func() error) {
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

// writeRefusal answers the way an ordinary web server would, so a prober that
// gets the path or the upgrade wrong learns nothing more.
func writeRefusal(writer io.Writer, status int) {
	fmt.Fprintf(writer, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", status, http.StatusText(status))
}

func headerContainsToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, element := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(element), token) {
				return true
			}
		}
	}
	return false
}
