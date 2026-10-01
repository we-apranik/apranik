package websocket

import (
	"fmt"
	"strings"

	"github.com/The-Apranik/Apranik/shared/websocket/handshake"
)

// Early data puts the start of the payload in the handshake, base64, so the
// server can begin the proxy request without waiting a round trip. The
// defaults match Xray: the Sec-WebSocket-Protocol header. sing-box's default,
// early data appended to the path, is chosen by setting the header name to "".
const defaultEarlyDataHeaderName = "Sec-WebSocket-Protocol"

// Base64 of the largest early data must still fit the server's handshake limit.
const maxEarlyDataLimit = 16 << 10

// One settings type for both roles: an outbound reads the client fields, an
// inbound the server ones, and each ignores the other's.
type Settings struct {
	// Both: the request path. A server compares it without the query string, as
	// Xray does, so a client's "?ed=2048" does not make it a different path.
	Path string `json:"path"`

	// Client: the Host header; empty means the server's address as dialed.
	// Server: if set, the Host a request must carry, port ignored; any other
	// gets a 400.
	Host string `json:"host"`

	// Client: extra request headers. No default: encoding/json merges into an
	// existing map, so a default entry could never be removed.
	Headers map[string]string `json:"headers"`

	// Client: how many bytes of the first write go in the handshake. 0, the
	// default, sends none. Server: with early_data_header_name set to "",
	// accept early data appended to the path (sing-box's path mode).
	MaxEarlyData int `json:"max_early_data"`

	// Both: the header that carries early data. A server always accepts early
	// data there, since a client that sends it loses those bytes otherwise.
	EarlyDataHeaderName string `json:"early_data_header_name"`
}

// Headers the module writes itself; setting them would break the handshake.
var headersTheModuleOwns = []string{
	"Host", "Upgrade", "Connection",
	"Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Accept", "Sec-WebSocket-Extensions",
}

func (s *Settings) Validate() error {
	if err := handshake.ValidatePath(s.Path); err != nil {
		return fmt.Errorf("websocket: %w", err)
	}
	if err := handshake.ValidateHost(s.Host); err != nil {
		return fmt.Errorf("websocket: %w", err)
	}
	if s.MaxEarlyData < 0 || s.MaxEarlyData > maxEarlyDataLimit {
		return fmt.Errorf("websocket: max_early_data %d must be between 0 and %d", s.MaxEarlyData, maxEarlyDataLimit)
	}
	if s.EarlyDataHeaderName != "" && !handshake.IsHeaderName(s.EarlyDataHeaderName) {
		return fmt.Errorf("websocket: early_data_header_name %q is not a valid header name", s.EarlyDataHeaderName)
	}
	if err := handshake.ValidateHeaders(s.Headers, headersTheModuleOwns); err != nil {
		return fmt.Errorf("websocket: headers: %w", err)
	}
	for name := range s.Headers {
		if s.MaxEarlyData > 0 && strings.EqualFold(name, s.EarlyDataHeaderName) {
			return fmt.Errorf("websocket: headers: %q carries early data and cannot also be set", name)
		}
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }
