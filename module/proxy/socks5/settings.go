package socks5

import (
	"errors"

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
}

func (s *Settings) Validate() error {
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
