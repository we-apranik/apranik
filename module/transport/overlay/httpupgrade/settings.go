package httpupgrade

import (
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/shared/websocket/handshake"
)

// One settings type for both roles: an outbound reads the client fields, an
// inbound the server ones, and each ignores the other's.
type Settings struct {
	// Both: the request path. A server compares it without the query string,
	// so an Xray client's "?ed=2048" does not make it a different path.
	Path string `json:"path"`

	// Client: the Host header; empty means the server's address as dialed.
	// Server: if set, the Host a request must carry, port ignored; any other
	// gets a 400.
	Host string `json:"host"`

	// Client: extra request headers. No default: encoding/json merges into an
	// existing map, so a default entry could never be removed.
	Headers map[string]string `json:"headers"`

	// Client: send the payload right after the request instead of waiting for
	// the server's 101, saving a round trip; Xray's "ed". Off by default:
	// Xray's and sing-box's servers can drop bytes that arrive with the
	// request, and the first payload is then lost.
	Pipeline bool `json:"pipeline"`

	// Server: accept a request that carries Sec-WebSocket-Key, as Xray does.
	// false answers it with a 404, as sing-box does.
	AcceptWebSocketKey bool `json:"accept_websocket_key"`

	// Both: how long the upgrade handshake may take, including a pipelining
	// client's wait for the 101 on its first Read. Zero means
	// defaultHandshakeTimeout.
	HandshakeTimeout types.Duration `json:"handshake_timeout"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

func (s *Settings) handshakeTimeout() time.Duration {
	if s.HandshakeTimeout == 0 {
		return defaultHandshakeTimeout
	}
	return time.Duration(s.HandshakeTimeout)
}

// Headers the module writes itself; setting them would break the handshake.
var headersTheModuleOwns = []string{"Host", "Upgrade", "Connection"}

func (s *Settings) Validate() error {
	if err := handshake.ValidatePath(s.Path); err != nil {
		return fmt.Errorf("httpupgrade: %w", err)
	}
	if err := handshake.ValidateHost(s.Host); err != nil {
		return fmt.Errorf("httpupgrade: %w", err)
	}
	if err := handshake.ValidateHeaders(s.Headers, headersTheModuleOwns); err != nil {
		return fmt.Errorf("httpupgrade: headers: %w", err)
	}
	if s.HandshakeTimeout < 0 {
		return fmt.Errorf("httpupgrade: handshake_timeout %s must not be negative", s.HandshakeTimeout)
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }
