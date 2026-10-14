package httpupgrade

import (
	"fmt"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/websocket/handshake"
	"github.com/we-apranik/apranik/spec"
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
	// client's wait for the 101 on its first Read. Absent means
	// defaultHandshakeTimeout; a written zero is refused.
	HandshakeTimeout *types.Duration `json:"handshake_timeout"`

	// Server: read the client's IP from a header a proxy in front added.
	OriginalSourceIP types.OriginalSourceIP `json:"original_source_ip"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
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
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("httpupgrade: handshake_timeout %w", err)
	}
	if err := s.OriginalSourceIP.Validate(); err != nil {
		return fmt.Errorf("httpupgrade: original_source_ip: %w", err)
	}
	return nil
}

func (s *Settings) Warnings() []string {
	var warnings []string
	for _, warning := range s.OriginalSourceIP.Warnings() {
		warnings = append(warnings, "httpupgrade: original_source_ip: "+warning)
	}
	return warnings
}

func (s *Settings) WarningsForListenAddress(listen spec.Endpoint) []string {
	return s.OriginalSourceIP.WarningsForListenAddress("httpupgrade", listen)
}
