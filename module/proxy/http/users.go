package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/we-apranik/apranik/spec"
)

var _ spec.UserManager = (*streamServer)(nil)

func (s *streamServer) AddUser(raw json.RawMessage) (string, error) {
	configured, err := decodeUser(raw)
	if err != nil {
		return "", err
	}
	if err := checkUser(configured); err != nil {
		return "", fmt.Errorf("http: %w: user %w", spec.ErrInvalidUser, err)
	}

	s.changeUsers.Lock()
	defer s.changeUsers.Unlock()

	current := *s.users.Load()
	if slices.ContainsFunc(current, func(existing user) bool { return existing.name == configured.Name }) {
		return "", fmt.Errorf("http: user %q: %w", configured.Name, spec.ErrUserExists)
	}

	next := make([]user, len(current), len(current)+1)
	copy(next, current)
	next = append(next, newUser(configured))
	s.users.Store(&next)
	return configured.Name, nil
}

func (s *streamServer) RemoveUser(name string) error {
	s.changeUsers.Lock()
	defer s.changeUsers.Unlock()

	current := *s.users.Load()
	index := slices.IndexFunc(current, func(existing user) bool { return existing.name == name })
	if index < 0 {
		return fmt.Errorf("http: user %q: %w", name, spec.ErrNoSuchUser)
	}

	next := make([]user, 0, len(current)-1)
	next = append(next, current[:index]...)
	next = append(next, current[index+1:]...)
	s.users.Store(&next)
	return nil
}

func (s *streamServer) ListUsers() []string {
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
		return User{}, fmt.Errorf("http: %w: %w", spec.ErrInvalidUser, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return User{}, fmt.Errorf("http: %w: more than one value", spec.ErrInvalidUser)
	}
	return configured, nil
}
