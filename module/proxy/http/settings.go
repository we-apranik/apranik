package http

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/spec"
)

type Settings struct {
	// RFC 7617 Basic proxy authentication. Both empty means none. On the
	// server the accepted name lands in Metadata.User.
	Username string       `json:"username"`
	Password types.Secret `json:"password"`

	// Both: how long the request and its answer may take. Absent means
	// defaultHandshakeTimeout; a written zero is refused.
	HandshakeTimeout *types.Duration `json:"handshake_timeout"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
}

func (s *Settings) Validate() error {
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("http: handshake_timeout %w", err)
	}
	if (s.Username == "") != (s.Password == "") {
		return errors.New("http: username and password must be set together or not at all")
	}
	if strings.Contains(s.Username, ":") {
		return errors.New("http: username may not contain ':', which Basic authentication uses to separate it from the password")
	}
	return nil
}

func (s *Settings) Warnings() []string {
	if s.Username == "" {
		return nil
	}
	// Basic auth is base64, not encryption. Over a bare TCP carrier the
	// credential crosses the wire recoverable by anyone on the path.
	return []string{"http: Basic authentication sends the password in a recoverable form unless the carrier below it is encrypted"}
}

// An inbound with no username is open to whoever can reach its port: only a
// listen address on this machine makes that safe.
func (s *Settings) WarningsForListenAddress(listen spec.Endpoint) []string {
	if s.Username == "" && !listen.Addr().IsLoopback() {
		return []string{"http: no username set, so any client that can reach this inbound may use it"}
	}
	return nil
}
