package socks5

import (
	"errors"
	"fmt"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/spec"
)

// No server address here: a proxy protocol never dials, so that belongs to the
// outbound stack.
type Settings struct {
	// RFC 1929 authentication. Both empty means no-auth and nothing else; on
	// the server the accepted name lands in Metadata.User.
	Username string       `json:"username"`
	Password types.Secret `json:"password"`

	// On by default, but still depends on core having granted a socket func:
	// without one the command is refused with X'07' whatever this says.
	UDPRelay bool `json:"udp_relay"`

	// The largest UDP datagram the server takes from a client through UDP
	// ASSOCIATE; a bigger one is dropped. Absent means defaultMaxDatagramSize; a
	// written zero is refused.
	// Match the inbound's udp_max_datagram_size, which bounds what core
	// relays. Server only.
	UDPMaxDatagramSize *int `json:"udp_max_datagram_size"`

	// Both: how long the handshake may take; on a server, UDP ASSOCIATE's
	// reply too. Absent means defaultHandshakeTimeout; a written zero is refused.
	HandshakeTimeout *types.Duration `json:"handshake_timeout"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

// Above what any network carries without fragmenting and above DNS's largest,
// and far below the 65507 a datagram can hold, so an association's buffer is
// 8 KB, not 64. PROVISIONAL; core's own default is the same.
const defaultMaxDatagramSize = 8192

// The most a datagram can hold over IPv4.
const largestDatagramSize = 65507

func (s *Settings) maxDatagramSize() int {
	return types.ValueOrDefault(s.UDPMaxDatagramSize, defaultMaxDatagramSize)
}

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
}

func (s *Settings) Validate() error {
	if err := types.CheckPositive(s.UDPMaxDatagramSize); err != nil {
		return fmt.Errorf("socks5: udp_max_datagram_size %w", err)
	}
	if s.UDPMaxDatagramSize != nil && *s.UDPMaxDatagramSize > largestDatagramSize {
		return fmt.Errorf("socks5: udp_max_datagram_size %d is more than a UDP datagram can hold (%d)", *s.UDPMaxDatagramSize, largestDatagramSize)
	}
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("socks5: handshake_timeout %w", err)
	}
	if (s.Username == "") != (s.Password == "") {
		return errors.New("socks5: username and password must be set together or not at all")
	}
	if len(s.Username) > 255 || len(s.Password) > 255 {
		return errors.New("socks5: username and password are limited to 255 bytes each")
	}
	return nil
}

func (s *Settings) Warnings() []string { return nil }

// An inbound with no username is open to whoever can reach its port: only a
// listen address on this machine makes that safe.
func (s *Settings) WarningsForListenAddress(listen spec.Endpoint) []string {
	if s.Username == "" && !listen.Addr().IsLoopback() {
		return []string{"socks5: no username set, so any client that can reach this inbound may use it"}
	}
	return nil
}
