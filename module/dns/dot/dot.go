package dot

import (
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/dnsstream"
	"github.com/we-apranik/apranik/spec"
)

const (
	DefaultPort = 853

	// PROVISIONAL.
	DefaultTimeout = 5 * time.Second
)

type Settings struct {
	Server types.ServerAddress `json:"server"`

	Timeout *types.Duration `json:"timeout"`
}

func (s *Settings) Validate() error {
	if s.Server.IsZero() {
		return errors.New("dot: server is required")
	}
	if err := types.CheckPositive(s.Timeout); err != nil {
		return fmt.Errorf("dot: timeout %w", err)
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }

type Server struct{ Settings }

func (*Server) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "dot", Description: "DNS over TLS (RFC 7858)"}
}

func (*Server) NeedsTLS() bool { return true }

func (s *Server) NewStreamDNSServer(dialer spec.StreamDialer) (spec.DNSServer, error) {
	if dialer == nil {
		return nil, errors.New("dot: no dialer")
	}
	destination, err := s.Server.Destination(DefaultPort)
	if err != nil {
		return nil, fmt.Errorf("dot: server: %w", err)
	}
	timeout := time.Duration(types.ValueOrDefault(s.Timeout, types.Duration(DefaultTimeout)))
	return dnsstream.NewClient(dialer, destination, timeout), nil
}

func NewStreamDNSServerModule() spec.StreamDNSServerModule { return &Server{} }
