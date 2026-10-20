package mixed

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/we-apranik/apranik/spec"
)

type userManagingStreamServer struct {
	*streamServer
	socks5Manager spec.UserManager
	httpManager   spec.UserManager

	// One change at a time, so two callers cannot leave the lists different.
	changeUsers sync.Mutex
}

var _ spec.UserManager = (*userManagingStreamServer)(nil)

func (s *userManagingStreamServer) AddUser(user json.RawMessage) (string, error) {
	s.changeUsers.Lock()
	defer s.changeUsers.Unlock()

	name, err := s.socks5Manager.AddUser(user)
	if err != nil {
		return "", fmt.Errorf("mixed: socks5: %w", err)
	}
	if _, err := s.httpManager.AddUser(user); err != nil {
		err = fmt.Errorf("mixed: http: %w", err)
		if undoErr := s.socks5Manager.RemoveUser(name); undoErr != nil {
			err = errors.Join(err, fmt.Errorf("mixed: socks5: undoing the user: %w", undoErr))
		}
		return "", err
	}
	return name, nil
}

func (s *userManagingStreamServer) RemoveUser(name string) error {
	s.changeUsers.Lock()
	defer s.changeUsers.Unlock()

	if err := s.socks5Manager.RemoveUser(name); err != nil {
		return fmt.Errorf("mixed: socks5: %w", err)
	}
	if err := s.httpManager.RemoveUser(name); err != nil && !errors.Is(err, spec.ErrNoSuchUser) {
		return fmt.Errorf("mixed: http: %w", err)
	}
	return nil
}

func (s *userManagingStreamServer) ListUsers() []string { return s.socks5Manager.ListUsers() }
