// Package udp is the DNS server type udp: plain DNS over UDP (RFC 1035). Each
// query gets its own datagram, so its own source port (RFC 5452), and only an
// answer from the server carrying the query's ID is taken; anything else that
// arrives -- a stray packet, a blind injection -- is ignored until the timeout.
package udp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	DefaultPort = 53

	// PROVISIONAL.
	DefaultTimeout = 5 * time.Second

	// Above the 1232 bytes core advertises with EDNS(0); a larger answer is
	// treated as truncated, which tells the user to use a stream server.
	responseBufferLength = 4096

	headerLength = 12
)

type Settings struct {
	// Where the DNS server is; the port defaults to DefaultPort. An IP: a
	// hostname here would have to be resolved by the resolver it serves.
	Server types.ServerAddress `json:"server"`

	// Bounds one query. Absent means DefaultTimeout; a written zero is refused.
	Timeout *types.Duration `json:"timeout"`
}

func (s *Settings) Validate() error {
	if s.Server.IsZero() {
		return errors.New("udp: server is required")
	}
	if err := types.CheckPositive(s.Timeout); err != nil {
		return fmt.Errorf("udp: timeout %w", err)
	}
	return nil
}

func (s *Settings) Warnings() []string {
	if destination, err := s.Server.Destination(DefaultPort); err == nil {
		if _, isDomain := destination.Domain(); isDomain {
			return []string{"udp: server is a hostname; give it as an IP, or give this server a dns_server to resolve it, or every lookup fails"}
		}
	}
	return nil
}

type Server struct{ Settings }

func (*Server) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "udp", Description: "DNS over UDP (RFC 1035)"}
}

func (s *Server) NewDatagramDNSServer(dialer spec.DatagramDialer) (spec.DNSServer, error) {
	if dialer == nil {
		return nil, errors.New("udp: no dialer")
	}
	destination, err := s.Server.Destination(DefaultPort)
	if err != nil {
		return nil, fmt.Errorf("udp: server: %w", err)
	}
	timeout := time.Duration(types.ValueOrDefault(s.Timeout, types.Duration(DefaultTimeout)))
	return &client{dialer: dialer, server: destination, timeout: timeout}, nil
}

func NewDatagramDNSServerModule() spec.DatagramDNSServerModule { return &Server{} }

type client struct {
	dialer  spec.DatagramDialer
	server  spec.Destination
	timeout time.Duration
}

func (c *client) Query(ctx context.Context, queryMessage []byte) ([]byte, error) {
	if len(queryMessage) < headerLength {
		return nil, errors.New("udp: a DNS message is at least 12 bytes")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	datagram, err := c.dialer.CreateDatagram(ctx, spec.Endpoint{})
	if err != nil {
		return nil, fmt.Errorf("udp: %w", err)
	}
	defer datagram.Close()
	// A Datagram has no deadline: closing it is what ends a read that waits.
	stopClosing := context.AfterFunc(ctx, func() { datagram.Close() })
	defer stopClosing()

	if _, err := datagram.WriteTo(queryMessage, c.server); err != nil {
		return nil, fmt.Errorf("udp: send to %s: %w", c.server, err)
	}

	buffer := make([]byte, responseBufferLength)
	for {
		length, from, err := datagram.ReadFrom(buffer)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, spec.ErrMessageTooLong) {
			return nil, spec.ErrDNSAnswerTruncated
		}
		if err != nil {
			return nil, fmt.Errorf("udp: read from %s: %w", c.server, err)
		}
		if c.answers(queryMessage, buffer[:length], from) {
			return bytes.Clone(buffer[:length]), nil
		}
	}
}

// answers: from the server, a response, with the query's ID. Core checks the
// question.
func (c *client) answers(queryMessage, response []byte, from spec.Destination) bool {
	if len(response) < headerLength || response[2]&0x80 == 0 {
		return false
	}
	if binary.BigEndian.Uint16(response) != binary.BigEndian.Uint16(queryMessage) {
		return false
	}
	if serverIP, isIP := c.server.IP(); isIP {
		fromIP, fromIsIP := from.IP()
		return fromIsIP && fromIP == serverIP && from.Port() == c.server.Port()
	}
	return true
}

// Holds nothing: each query's datagram is closed with it.
func (c *client) Close() error { return nil }
