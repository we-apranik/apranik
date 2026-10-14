package trojan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/we-apranik/apranik/spec"
)

var _ spec.UserManager = (*server)(nil)

// AddUser adds a user to the running server. The object is a user as the
// config file writes it, with unknown keys refused. The change applies to the
// next handshake.
func (s *server) AddUser(raw json.RawMessage) (string, error) {
	configured, err := decodeUser(raw)
	if err != nil {
		return "", err
	}
	if err := checkUser(configured); err != nil {
		return "", fmt.Errorf("trojan: %w: user %w", spec.ErrInvalidUser, err)
	}
	added := newUser(configured)

	s.changeUsers.Lock()
	defer s.changeUsers.Unlock()

	current := *s.users.Load()
	for _, existing := range current {
		if existing.name == added.name {
			return "", fmt.Errorf("trojan: user %q: %w", added.name, spec.ErrUserExists)
		}
		// A shared password would make the matched user ambiguous.
		if existing.passwordHash == added.passwordHash {
			return "", fmt.Errorf("trojan: %w: user %q has the same password as another user", spec.ErrInvalidUser, added.name)
		}
	}

	next := make([]user, len(current), len(current)+1)
	copy(next, current)
	next = append(next, added)
	s.users.Store(&next)
	return added.name, nil
}

// RemoveUser removes a user from the running server. The inbound may be left
// with no users, and then refuses every client until one is added.
func (s *server) RemoveUser(name string) error {
	s.changeUsers.Lock()
	defer s.changeUsers.Unlock()

	current := *s.users.Load()
	index := slices.IndexFunc(current, func(existing user) bool { return existing.name == name })
	if index < 0 {
		return fmt.Errorf("trojan: user %q: %w", name, spec.ErrNoSuchUser)
	}

	next := make([]user, 0, len(current)-1)
	next = append(next, current[:index]...)
	next = append(next, current[index+1:]...)
	s.users.Store(&next)
	return nil
}

func (s *server) ListUsers() []string {
	current := *s.users.Load()
	names := make([]string, len(current))
	for index, existing := range current {
		names[index] = existing.name
	}
	return names
}

// The error never repeats a value from the object, so a password mistyped into
// the wrong field is not copied into a log line.
func decodeUser(raw json.RawMessage) (User, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var configured User
	if err := decoder.Decode(&configured); err != nil {
		return User{}, fmt.Errorf("trojan: %w: %w", spec.ErrInvalidUser, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return User{}, fmt.Errorf("trojan: %w: more than one value", spec.ErrInvalidUser)
	}
	return configured, nil
}
