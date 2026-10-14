// Package tcp is the DNS server type tcp: DNS over TCP (RFC 7766), one
// connection reused and queries pipelined on it.
package tcp

import (
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/shared/dnsstream"
	"github.com/we-apranik/apranik/spec"
)

const (
	DefaultPort = 53

	// PROVISIONAL.
	DefaultTimeout = 5 * time.Second
)

type Settings struct {
	// Where the DNS server is; the port defaults to DefaultPort.
	Server types.ServerAddress `json:"server"`

	// Bounds one query, the dial included. Absent means DefaultTimeout; a written zero is refused.
	Timeout *types.Duration `json:"timeout"`
}

func (s *Settings) Validate() error {
	if s.Server.IsZero() {
		return errors.New("tcp: server is required")
	}
	if err := types.CheckPositive(s.Timeout); err != nil {
		return fmt.Errorf("tcp: timeout %w", err)
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }

type Server struct{ Settings }

func (*Server) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "tcp", Description: "DNS over TCP (RFC 7766)"}
}

func (*Server) NeedsTLS() bool { return false }

func (s *Server) NewStreamDNSServer(dialer spec.StreamDialer) (spec.DNSServer, error) {
	if dialer == nil {
		return nil, errors.New("tcp: no dialer")
	}
	destination, err := s.Server.Destination(DefaultPort)
	if err != nil {
		return nil, fmt.Errorf("tcp: server: %w", err)
	}
	timeout := time.Duration(types.ValueOrDefault(s.Timeout, types.Duration(DefaultTimeout)))
	return dnsstream.NewClient(dialer, destination, timeout), nil
}

func NewStreamDNSServerModule() spec.StreamDNSServerModule { return &Server{} }
