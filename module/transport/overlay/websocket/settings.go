package websocket

import (
	"fmt"
	"strings"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/websocket/handshake"
	"github.com/we-apranik/apranik/spec"
)

// Early data puts the start of the payload in the handshake, base64, so the
// server can begin the proxy request without waiting a round trip. Xray puts
// it in the Sec-WebSocket-Protocol header; sing-box, by default, appends it to
// the path.
const defaultEarlyDataHeaderName = "Sec-WebSocket-Protocol"

const (
	earlyDataInHeader = "header"
	earlyDataInPath   = "path"
)

const maxEarlyDataLimit = 16 << 10

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

type Settings struct {
	// Both: the request path. A server compares it without the query string, as
	// Xray does, so a client's "?ed=2048" does not make it a different path.
	Path string `json:"path"`

	Host string `json:"host"`

	// Client: extra request headers. No default: encoding/json merges into an
	// existing map, so a default entry could never be removed.
	Headers map[string]string `json:"headers"`

	EarlyData *EarlyData `json:"early_data"`

	HandshakeTimeout *types.Duration `json:"handshake_timeout"`

	OriginalSourceIP types.OriginalSourceIP `json:"original_source_ip"`
}

type EarlyData struct {
	// Client: how many bytes of the first write go in the handshake. Server:
	// the most it accepts. Required: Xray and sing-box have no default either.
	MaxBytes int `json:"max_bytes"`

	Location string `json:"location"`

	HeaderName string `json:"header_name"`
}

func (e *EarlyData) isInPath() bool { return e.Location == earlyDataInPath }

func (e *EarlyData) effectiveHeaderName() string {
	if e.HeaderName == "" {
		return defaultEarlyDataHeaderName
	}
	return e.HeaderName
}

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
}

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
	if err := handshake.ValidateHeaders(s.Headers, headersTheModuleOwns); err != nil {
		return fmt.Errorf("websocket: headers: %w", err)
	}
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("websocket: handshake_timeout %w", err)
	}
	if s.EarlyData != nil {
		if err := s.EarlyData.validate(s.Headers); err != nil {
			return fmt.Errorf("websocket: early_data: %w", err)
		}
	}
	if err := s.OriginalSourceIP.Validate(); err != nil {
		return fmt.Errorf("websocket: original_source_ip: %w", err)
	}
	return nil
}

func (e *EarlyData) validate(headers map[string]string) error {
	if e.MaxBytes < 1 || e.MaxBytes > maxEarlyDataLimit {
		return fmt.Errorf("max_bytes %d must be between 1 and %d", e.MaxBytes, maxEarlyDataLimit)
	}
	switch e.Location {
	case "", earlyDataInHeader:
	case earlyDataInPath:
		if e.HeaderName != "" {
			return fmt.Errorf("header_name %q needs location %q, not %q", e.HeaderName, earlyDataInHeader, earlyDataInPath)
		}
		return nil
	default:
		return fmt.Errorf("location %q is not %q or %q", e.Location, earlyDataInHeader, earlyDataInPath)
	}
	if e.HeaderName != "" && !handshake.IsHeaderName(e.HeaderName) {
		return fmt.Errorf("header_name %q is not a valid header name", e.HeaderName)
	}
	for name := range headers {
		if strings.EqualFold(name, e.effectiveHeaderName()) {
			return fmt.Errorf("%q carries early data, so headers cannot also set it", name)
		}
	}
	return nil
}

func (s *Settings) Warnings() []string {
	var warnings []string
	for _, warning := range s.OriginalSourceIP.Warnings() {
		warnings = append(warnings, "websocket: original_source_ip: "+warning)
	}
	return warnings
}

func (s *Settings) WarningsForListenAddress(listen spec.Endpoint) []string {
	return s.OriginalSourceIP.WarningsForListenAddress("websocket", listen)
}
