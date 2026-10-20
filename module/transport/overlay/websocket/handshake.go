package websocket

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"strings"
)

// RFC 6455 section 1.3.
const acceptKeySuffix = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var (
	errNotAnUpgrade     = errors.New("websocket: not a WebSocket upgrade request")
	errWrongHost        = errors.New("websocket: wrong host")
	errWrongPath        = errors.New("websocket: wrong path")
	errUpgradeRefused   = errors.New("websocket: the server did not switch protocols")
	errWrongAcceptKey   = errors.New("websocket: the server's Sec-WebSocket-Accept does not match the key")
	errBadEarlyData     = errors.New("websocket: early data is not valid base64")
	errTooMuchEarlyData = errors.New("websocket: more early data than max_bytes")
)

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
