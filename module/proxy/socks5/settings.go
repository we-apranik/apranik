package socks5

import (
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/controlcharacter"
	"github.com/we-apranik/apranik/spec"
)

type Settings struct {
	// What a client sends (RFC 1929), set together or not at all; an inbound
	// refuses them, it takes users.
	Username string       `json:"username"`
	Password types.Secret `json:"password"`

	// Who an inbound accepts, by RFC 1929 username and password; the matched
	// name lands in Metadata.User. An inbound with none accepts everyone, with
	// no authentication. An outbound refuses them. Users can be added and
	// removed while the inbound runs (UserManager).
	Users []User `json:"users"`

	// On by default, but still depends on core having granted a socket func:
	// without one the command is refused with X'07' whatever this says.
	UDPRelay bool `json:"udp_relay"`

	UDPMaxDatagramSize *int `json:"udp_max_datagram_size"`

	HandshakeTimeout *types.Duration `json:"handshake_timeout"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

// Above what any network carries without fragmenting and above DNS's largest,
// and far below the 65507 a datagram can hold, so an association's buffer is
// 8 KB, not 64. PROVISIONAL; core's own default is the same.
const defaultMaxDatagramSize = 8192

const largestDatagramSize = 65507

func (s *Settings) maxDatagramSize() int {
	return types.ValueOrDefault(s.UDPMaxDatagramSize, defaultMaxDatagramSize)
}

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
}

func (s *Settings) Validate() error {
	if err := types.CheckPositive(s.UDPMaxDatagramSize); err != nil {
		return fmt.Errorf("socks5: udp_max_datagram_size %w", err)
	}
	if s.UDPMaxDatagramSize != nil && *s.UDPMaxDatagramSize > largestDatagramSize {
		return fmt.Errorf("socks5: udp_max_datagram_size %d is more than a UDP datagram can hold (%d)", *s.UDPMaxDatagramSize, largestDatagramSize)
	}
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("socks5: handshake_timeout %w", err)
	}
	if (s.Username == "") != (s.Password == "") {
		return errors.New("socks5: username and password must be set together or not at all")
	}
	if len(s.Username) > 255 || len(s.Password) > 255 {
		return errors.New("socks5: username and password are limited to 255 bytes each")
	}

	names := make(map[string]int, len(s.Users))
	for index, user := range s.Users {
		if err := checkUser(user); err != nil {
			return fmt.Errorf("socks5: users[%d] %w", index, err)
		}
		if first, seen := names[user.Name]; seen {
			return fmt.Errorf("socks5: users[%d] and users[%d] are both named %q", first, index, user.Name)
		}
		names[user.Name] = index
	}
	return nil
}

type User struct {
	Name     string       `json:"name"`
	Password types.Secret `json:"password"`
}

func checkUser(user User) error {
	switch {
	case user.Name == "":
		return errors.New("has no name")
	case user.Password == "":
		return fmt.Errorf("%q has no password", user.Name)
	case len(user.Name) > 255 || len(user.Password) > 255:
		return fmt.Errorf("%q is over 255 bytes, which RFC 1929 limits a username and a password to", user.Name)
	case controlcharacter.Contains(user.Name):
		return fmt.Errorf("%q has a control character in its name", user.Name)
	case controlcharacter.Contains(user.Password.Reveal()):
		return fmt.Errorf("%q has a control character in its password", user.Name)
	}
	return nil
}

func (s *Settings) checkAsServer() error {
	if s.Username != "" || s.Password != "" {
		return errors.New(`socks5: an inbound takes users, not username and password: "users": [{"name": "...", "password": "..."}]`)
	}
	return nil
}

func (s *Settings) checkAsClient() error {
	if len(s.Users) > 0 {
		return errors.New("socks5: an outbound takes username and password, not users")
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }

func (s *Settings) WarningsForListenAddress(listen spec.Endpoint) []string {
	if len(s.Users) == 0 && !listen.Addr().IsLoopback() {
		return []string{"socks5: no users, so any client that can reach this inbound may use it"}
	}
	return nil
}
