// Package dot is the DNS server type dot: DNS over TLS (RFC 7858), one
// connection reused and queries pipelined on it. The TLS layer is core's: the
// registry's tls module, built from the server's tls field, under the stream.
package dot

import (
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/shared/dnsstream"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	DefaultPort = 853

	// PROVISIONAL.
	DefaultTimeout = 5 * time.Second
)

type Settings struct {
	// Where the DNS server is; the port defaults to DefaultPort.
	Server types.ServerAddress `json:"server"`

	// Bounds one query, the dial included. Zero means DefaultTimeout.
	Timeout types.Duration `json:"timeout"`
}

func (s *Settings) Validate() error {
	if s.Server.IsZero() {
		return errors.New("dot: server is required")
	}
	if s.Timeout < 0 {
		return fmt.Errorf("dot: timeout %s must not be negative", s.Timeout)
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
	timeout := time.Duration(s.Timeout)
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return dnsstream.NewClient(dialer, destination, timeout), nil
}

func NewStreamDNSServerModule() spec.StreamDNSServerModule { return &Server{} }
