package udp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// The largest a UDP payload can be over IPv4. A caller whose buffer is at
// least this big cannot be handed a truncated datagram.
const maxDatagram = 65507

// Domains only appear here when a proxy layer passed one through unresolved,
// so the working set is small; this bounds it so a long session can't grow it
// without limit.
const resolveCacheLimit = 256

type resolverPolicy struct {
	timeout  time.Duration
	cacheTTL time.Duration
}

type resolvedAddress struct {
	address *net.UDPAddr
	expires time.Time
}

// Safe for one reader and many writers, which is what a relay needs.
type socket struct {
	connection *net.UDPConn

	// "udp", "udp4" or "udp6": which of a domain's addresses this socket can
	// actually reach.
	network string

	// Used only when a caller's buffer is too small to rule out truncation,
	// so the relay's full-size reads stay copy-free. One reader, so one
	// buffer.
	oversizeBuffer []byte

	resolver resolverPolicy

	mu       sync.Mutex
	resolved map[string]resolvedAddress
}

func newSocket(connection *net.UDPConn, network string, resolver resolverPolicy) *socket {
	return &socket{
		connection: connection,
		network:    network,
		resolver:   resolver,
		resolved:   make(map[string]resolvedAddress),
	}
}

// A sender is always a resolved address, so this never yields a domain.
//
// A datagram larger than buffer would otherwise be truncated with no error at
// all, so a short buffer reads through an internal one and reports the loss.
func (s *socket) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	target := buffer
	if len(buffer) < maxDatagram {
		if s.oversizeBuffer == nil {
			s.oversizeBuffer = make([]byte, maxDatagram)
		}
		target = s.oversizeBuffer
	}

	size, sender, err := s.connection.ReadFromUDPAddrPort(target)
	if err != nil {
		return 0, spec.Destination{}, err
	}

	from, err := spec.NewDestinationFromIP(sender.Addr().Unmap(), sender.Port())
	if err != nil {
		return 0, spec.Destination{}, fmt.Errorf("udp: sender %s: %w", sender, err)
	}

	if len(buffer) < maxDatagram {
		if size > len(buffer) {
			return 0, from, fmt.Errorf("udp: %d byte datagram into a %d byte buffer: %w",
				size, len(buffer), spec.ErrMessageTooLong)
		}
		copy(buffer, target[:size])
	}
	return size, from, nil
}

func (s *socket) WriteTo(payload []byte, to spec.Destination) (int, error) {
	size, _, err := s.WriteToResolved(payload, to)
	return size, err
}

// WriteToResolved is spec.ResolvedWriter: this is the layer that resolves, so
// it is the only one that can say which address a domain actually went to.
func (s *socket) WriteToResolved(payload []byte, to spec.Destination) (int, spec.Endpoint, error) {
	target, err := s.resolve(to)
	if err != nil {
		return 0, spec.Endpoint{}, err
	}

	sentTo := spec.NewEndpointFromNetAddr(target)

	size, err := s.connection.WriteToUDP(payload, target)
	if errors.Is(err, syscall.EMSGSIZE) {
		return size, sentTo, fmt.Errorf("udp: %d bytes to %s: %w", len(payload), target, spec.ErrMessageTooLong)
	}
	return size, sentTo, err
}

func (s *socket) Close() error { return s.connection.Close() }

func (s *socket) LocalAddr() net.Addr { return s.connection.LocalAddr() }

// An IP needs no lookup at all; only a domain reaches the resolver, and its
// result is cached briefly because a session usually revisits the same few.
func (s *socket) resolve(to spec.Destination) (*net.UDPAddr, error) {
	if !to.IsValid() {
		return nil, errors.New("udp: address has neither domain nor IP")
	}
	if to.Port() == 0 {
		return nil, errors.New("udp: address has no port")
	}

	if ip, ok := to.IP(); ok {
		return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, to.Port())), nil
	}

	domain, _ := to.Domain()
	if cached, found := s.cached(domain); found {
		return cached, nil
	}

	ctx, done := context.WithTimeout(context.Background(), s.resolver.timeout)
	defer done()

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", domain)
	if err != nil {
		return nil, fmt.Errorf("udp: resolve %s: %w", domain, err)
	}

	chosen, err := reachable(addresses, s.network)
	if err != nil {
		return nil, fmt.Errorf("udp: resolve %s: %w", domain, err)
	}

	target := net.UDPAddrFromAddrPort(netip.AddrPortFrom(chosen, to.Port()))
	s.cache(domain, target)
	return target, nil
}

func (s *socket) cached(domain string) (*net.UDPAddr, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, found := s.resolved[domain]
	if !found {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		delete(s.resolved, domain)
		return nil, false
	}
	return entry.address, true
}

// Expired entries are swept before anything live is dropped, so a session that
// revisits the same handful of domains never loses one of them to a burst.
func (s *socket) cache(domain string, target *net.UDPAddr) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.resolved) >= resolveCacheLimit {
		now := time.Now()
		for key, entry := range s.resolved {
			if now.After(entry.expires) {
				delete(s.resolved, key)
			}
		}
	}
	for key := range s.resolved {
		if len(s.resolved) < resolveCacheLimit {
			break
		}
		delete(s.resolved, key)
	}

	s.resolved[domain] = resolvedAddress{address: target, expires: time.Now().Add(s.resolver.cacheTTL)}
}

// A domain with both an A and an AAAA record yields both, in whatever order
// the resolver chose, and a socket bound to one family cannot reach the other:
// sending there is not an error, the datagram simply vanishes. So the family
// decides, and a dual-stack socket prefers IPv4, which is what this transport
// resolved to before and what a host with no IPv6 route needs.
func reachable(addresses []netip.Addr, network string) (netip.Addr, error) {
	var first4, first6 netip.Addr
	for _, address := range addresses {
		unmapped := address.Unmap()
		switch {
		case unmapped.Is4() && !first4.IsValid():
			first4 = unmapped
		case !unmapped.Is4() && !first6.IsValid():
			first6 = unmapped
		}
	}

	switch network {
	case "udp4":
		if !first4.IsValid() {
			return netip.Addr{}, errors.New("no IPv4 address, and this socket is IPv4-only")
		}
		return first4, nil
	case "udp6":
		if !first6.IsValid() {
			return netip.Addr{}, errors.New("no IPv6 address, and this socket is IPv6-only")
		}
		return first6, nil
	default:
		if first4.IsValid() {
			return first4, nil
		}
		if first6.IsValid() {
			return first6, nil
		}
		return netip.Addr{}, errors.New("no addresses")
	}
}
