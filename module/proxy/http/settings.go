package http

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/controlcharacter"
	"github.com/we-apranik/apranik/spec"
)

type Settings struct {
	// What a client sends (RFC 7617 Basic proxy authentication), set together
	// or not at all; an inbound refuses them, it takes users.
	Username string       `json:"username"`
	Password types.Secret `json:"password"`

	// Who an inbound accepts, by Basic proxy authentication; the matched name
	// lands in Metadata.User. An inbound with none accepts everyone, with no
	// authentication. An outbound refuses them. Users can be added and removed
	// while the inbound runs (UserManager).
	Users []User `json:"users"`

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

	names := make(map[string]int, len(s.Users))
	for index, user := range s.Users {
		if err := checkUser(user); err != nil {
			return fmt.Errorf("http: users[%d] %w", index, err)
		}
		if first, seen := names[user.Name]; seen {
			return fmt.Errorf("http: users[%d] and users[%d] are both named %q", first, index, user.Name)
		}
		names[user.Name] = index
	}
	return nil
}

type User struct {
	Name     string       `json:"name"`
	Password types.Secret `json:"password"`
}

// checkUser says what is wrong with one user on its own, without saying where
// the user is and without ever naming a password.
func checkUser(user User) error {
	switch {
	case user.Name == "":
		return errors.New("has no name")
	case user.Password == "":
		return fmt.Errorf("%q has no password", user.Name)
	case strings.Contains(user.Name, ":"):
		return fmt.Errorf("%q has a ':' in its name, which Basic authentication uses to separate the name from the password", user.Name)
	case controlcharacter.Contains(user.Name):
		return fmt.Errorf("%q has a control character in its name", user.Name)
	case controlcharacter.Contains(user.Password.Reveal()):
		return fmt.Errorf("%q has a control character in its password", user.Name)
	}
	return nil
}

func (s *Settings) checkAsServer() error {
	if s.Username != "" || s.Password != "" {
		return errors.New(`http: an inbound takes users, not username and password: "users": [{"name": "...", "password": "..."}]`)
	}
	return nil
}

func (s *Settings) checkAsClient() error {
	if len(s.Users) > 0 {
		return errors.New("http: an outbound takes username and password, not users")
	}
	return nil
}

func (s *Settings) Warnings() []string {
	if s.Username == "" && len(s.Users) == 0 {
		return nil
	}
	// Basic auth is base64, not encryption. Over a bare TCP carrier the
	// credential crosses the wire recoverable by anyone on the path.
	return []string{"http: Basic authentication sends the password in a recoverable form unless the carrier below it is encrypted"}
}

// An inbound with no users is open to whoever can reach its port: only a
// listen address on this machine makes that safe.
func (s *Settings) WarningsForListenAddress(listen spec.Endpoint) []string {
	if len(s.Users) == 0 && !listen.Addr().IsLoopback() {
		return []string{"http: no users, so any client that can reach this inbound may use it"}
	}
	return nil
}
