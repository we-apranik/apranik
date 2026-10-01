package http

import (
	"errors"

	"github.com/The-Apranik/Apranik/config/types"
)

type Settings struct {
	// RFC 7617 Basic proxy authentication. Both empty means none. On the
	// server the accepted name lands in Metadata.User.
	Username string       `json:"username"`
	Password types.Secret `json:"password"`
}

func (s *Settings) Validate() error {
	if (s.Username == "") != (s.Password == "") {
		return errors.New("http: username and password must be set together or not at all")
	}
	return nil
}

func (s *Settings) Warnings() []string {
	if s.Username == "" {
		return []string{"http: no username set, so any client that can reach this inbound may use it"}
	}
	// Basic auth is base64, not encryption. Over a bare TCP carrier the
	// credential crosses the wire recoverable by anyone on the path.
	return []string{"http: Basic authentication sends the password in a recoverable form unless the carrier below it is encrypted"}
}
