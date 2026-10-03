package socks5

import (
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
)

// No server address here: a proxy protocol never dials, so that belongs to the
// outbound stack.
type Settings struct {
	// RFC 1929 authentication. Both empty means no-auth and nothing else; on
	// the server the accepted name lands in Metadata.User.
	Username string       `json:"username"`
	Password types.Secret `json:"password"`

	// On by default, but still depends on core having granted a socket func:
	// without one the command is refused with X'07' whatever this says.
	UDPRelay bool `json:"udp_relay"`

	// Both: how long the handshake may take; on a server, UDP ASSOCIATE's
	// reply too. Zero means defaultHandshakeTimeout.
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

func (s *Settings) Validate() error {
	if s.HandshakeTimeout < 0 {
		return fmt.Errorf("socks5: handshake_timeout %s must not be negative", s.HandshakeTimeout)
	}
	if (s.Username == "") != (s.Password == "") {
		return errors.New("socks5: username and password must be set together or not at all")
	}
	if len(s.Username) > 255 || len(s.Password) > 255 {
		return errors.New("socks5: username and password are limited to 255 bytes each")
	}
	return nil
}

func (s *Settings) Warnings() []string {
	if s.Username == "" {
		return []string{"socks5: no username set, so any client that can reach this inbound may use it"}
	}
	return nil
}
