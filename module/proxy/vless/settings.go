package vless

import (
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/proxy/vless/wire"
	"github.com/The-Apranik/Apranik/shared/controlcharacter"
)

// No server address here: a proxy protocol never dials, so that belongs to the
// outbound stack. VLESS has no encryption of its own; without an encrypting
// transport below it, the UUID and all payload cross the wire in the clear.
type Settings struct {
	// What an outbound sends. An inbound refuses it: it takes users. Written
	// as Xray and sing-box accept it: a UUID, or up to 30 bytes of any text,
	// which becomes one.
	UUID types.Secret `json:"uuid"`

	// Who an inbound accepts; an outbound refuses it. The matched user's name
	// lands in Metadata.User. Users can be added and removed while the inbound
	// runs (UserManager), and a name is unique within the inbound.
	Users []User `json:"users"`

	// Server: how long a client may take to send its request. A client sends
	// its request with the first payload and waits for no answer, so it has
	// nothing to bound. Absent means defaultHandshakeTimeout; a written zero is refused.
	HandshakeTimeout *types.Duration `json:"handshake_timeout"`

	// Client: the longest the request waits for the application's first bytes
	// to travel with it (a server-first protocol such as SSH sends nothing, so
	// after this the request goes alone). An inbound refuses it. Absent means
	// defaultMaxFirstPayloadDelay; a written zero is refused.
	MaxFirstPayloadDelay *types.Duration `json:"max_first_payload_delay"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

// PROVISIONAL: 400 ms. Xray waits 500 ms in its VLESS client and 100 ms in its
// Trojan, VMess and Shadowsocks clients, sing-box 300 ms; none of them says why.
const defaultMaxFirstPayloadDelay = 400 * time.Millisecond

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
}

func (s *Settings) maxFirstPayloadDelay() time.Duration {
	return time.Duration(types.ValueOrDefault(s.MaxFirstPayloadDelay, types.Duration(defaultMaxFirstPayloadDelay)))
}

type User struct {
	Name string       `json:"name"`
	UUID types.Secret `json:"uuid"`
}

// Validate holds what is wrong for either role; checkAsServer and checkAsClient
// hold what is wrong for one, and run where the role is known.
func (s *Settings) Validate() error {
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("vless: handshake_timeout %w", err)
	}
	if err := types.CheckPositive(s.MaxFirstPayloadDelay); err != nil {
		return fmt.Errorf("vless: max_first_payload_delay %w", err)
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

// checkUser says what is wrong with one user on its own, without saying where
// the user is and without ever naming a UUID.
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
	if s.MaxFirstPayloadDelay != nil {
		return errors.New("vless: max_first_payload_delay is for an outbound; an inbound has no request to send")
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
