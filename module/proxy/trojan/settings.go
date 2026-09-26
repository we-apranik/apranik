package trojan

import (
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/proxy/trojan/wire"
)

// No server address here: a proxy protocol never dials, so that belongs to the
// outbound stack. Trojan has no encryption of its own; without an encrypting
// transport below it, the password hash and all payload cross the wire in
// the clear.
type Settings struct {
	// What an outbound sends. An inbound ignores it.
	Password types.Secret `json:"password"`

	// Who an inbound accepts. The matched user's name lands in Metadata.User.
	// PROVISIONAL: user management is not designed yet, and this list may move.
	Users []User `json:"users"`
}

type User struct {
	Name     string       `json:"name"`
	Password types.Secret `json:"password"`
}

func (s *Settings) Validate() error {
	if s.Password == "" && len(s.Users) == 0 {
		return errors.New("trojan: set password for an outbound, or users for an inbound")
	}

	names := make(map[string]int, len(s.Users))
	hashes := make(map[wire.PasswordHash]int, len(s.Users))
	for index, user := range s.Users {
		if user.Name == "" {
			return fmt.Errorf("trojan: users[%d] has no name", index)
		}
		if user.Password == "" {
			return fmt.Errorf("trojan: user %q has no password", user.Name)
		}
		if first, seen := names[user.Name]; seen {
			return fmt.Errorf("trojan: users[%d] and users[%d] are both named %q", first, index, user.Name)
		}
		names[user.Name] = index

		// A shared password would make the matched user ambiguous.
		hash := wire.HashPassword(user.Password.Reveal())
		if first, seen := hashes[hash]; seen {
			return fmt.Errorf("trojan: users[%d] and users[%d] have the same password", first, index)
		}
		hashes[hash] = index
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }
