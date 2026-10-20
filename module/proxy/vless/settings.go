package vless

import (
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/module/proxy/vless/wire"
	"github.com/we-apranik/apranik/shared/controlcharacter"
)

// No server address here: a proxy protocol never dials, so that belongs to the
// outbound stack. VLESS has no encryption of its own; without an encrypting
// transport below it, the UUID and all payload cross the wire in the clear.
type Settings struct {
	// What an outbound sends. An inbound refuses it: it takes users. Written
	// as Xray and sing-box accept it: a UUID, or up to 30 bytes of any text,
	// which becomes one.
	UUID types.Secret `json:"uuid"`

	Users []User `json:"users"`

	HandshakeTimeout *types.Duration `json:"handshake_timeout"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
}

type User struct {
	Name string       `json:"name"`
	UUID types.Secret `json:"uuid"`
}

func (s *Settings) Validate() error {
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("vless: handshake_timeout %w", err)
	}
	if s.UUID == "" && len(s.Users) == 0 {
		return errors.New("vless: set uuid for an outbound, or users for an inbound")
	}
	if s.UUID != "" {
		if _, err := wire.ParseUUID(s.UUID.Reveal()); err != nil {
			return errors.New("vless: uuid is not a UUID, nor 1 to 30 bytes of text")
		}
	}

	names := make(map[string]int, len(s.Users))
	uuids := make(map[wire.UUID]int, len(s.Users))
	for index, user := range s.Users {
		if err := checkUser(user); err != nil {
			return fmt.Errorf("vless: users[%d] %w", index, err)
		}
		if first, seen := names[user.Name]; seen {
			return fmt.Errorf("vless: users[%d] and users[%d] are both named %q", first, index, user.Name)
		}
		names[user.Name] = index

		// The same UUID would make the matched user ambiguous.
		parsed, _ := wire.ParseUUID(user.UUID.Reveal())
		if first, seen := uuids[parsed]; seen {
			return fmt.Errorf("vless: users[%d] and users[%d] have the same uuid", first, index)
		}
		uuids[parsed] = index
	}
	return nil
}

func checkUser(user User) error {
	switch {
	case user.Name == "":
		return errors.New("has no name")
	case user.UUID == "":
		return fmt.Errorf("%q has no uuid", user.Name)
	case controlcharacter.Contains(user.Name):
		return fmt.Errorf("%q has a control character in its name", user.Name)
	}
	if _, err := wire.ParseUUID(user.UUID.Reveal()); err != nil {
		return fmt.Errorf("%q has a uuid that is not a UUID, nor 1 to 30 bytes of text", user.Name)
	}
	return nil
}

func (s *Settings) checkAsServer() error {
	if s.UUID != "" {
		return errors.New(`vless: an inbound takes users, not uuid: "users": [{"name": "...", "uuid": "..."}]`)
	}
	if len(s.Users) == 0 {
		return errors.New("vless: an inbound needs users")
	}
	return nil
}

func (s *Settings) checkAsClient() error {
	if len(s.Users) > 0 {
		return errors.New("vless: an outbound takes a uuid, not users")
	}
	if s.UUID == "" {
		return errors.New("vless: an outbound needs a uuid")
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }
