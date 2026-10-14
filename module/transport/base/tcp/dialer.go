package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/we-apranik/apranik/spec"
)

type streamDialer struct {
	dialer           net.Dialer
	resolver         spec.Resolver
	noDelay          bool
	dialTimeout      time.Duration
	nextAddressDelay time.Duration
}

// *net.TCPConn is returned unwrapped: it's already a spec.Stream, since
// net.Conn carries everything but CloseWrite and TCPConn has that.
//
// This is the only layer that resolves, and only its own target: a domain goes
// to the resolver it was given, never to the operating system directly, and
// dial_timeout covers the lookup and every attempt.
func (d *streamDialer) DialStream(ctx context.Context, destination spec.Destination) (spec.Stream, error) {
	if !destination.IsValid() {
		return nil, errors.New("tcp: no destination")
	}
	if destination.Port() == 0 {
		return nil, fmt.Errorf("tcp: address %s has no port", destination.Host())
	}

	ctx, cancel := context.WithTimeout(ctx, d.dialTimeout)
	defer cancel()

	targets, err := d.dialTargets(ctx, destination)
	if err != nil {
		return nil, err
	}
	connection, err := d.dialFirstToAnswer(ctx, targets)
	if err != nil {
		return nil, fmt.Errorf("tcp: dial %s: %w", destination, err)
	}

	tcpConnection, ok := connection.(*net.TCPConn)
	if !ok {
		connection.Close()
		return nil, fmt.Errorf("tcp: dial %s: got %T, want *net.TCPConn", destination, connection)
	}
	tcpConnection.SetNoDelay(d.noDelay)
	return tcpConnection, nil
}

func (d *streamDialer) dialTargets(ctx context.Context, destination spec.Destination) ([]netip.AddrPort, error) {
	if ip, isIP := destination.IP(); isIP {
		return []netip.AddrPort{netip.AddrPortFrom(ip, destination.Port())}, nil
	}
	domain, _ := destination.Domain()
	addresses, err := d.resolver.ResolveDomain(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("tcp: resolve %s: %w", domain, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("tcp: resolve %s: no addresses", domain)
	}
	targets := make([]netip.AddrPort, 0, len(addresses))
	for _, address := range interleaveAddressFamilies(addresses) {
		targets = append(targets, netip.AddrPortFrom(address.Unmap(), destination.Port()))
	}
	return targets, nil
}

// interleaveAddressFamilies alternates IPv4 and IPv6, starting with the family
// the resolver put first, so one broken family costs one attempt's delay, not
// one per address (RFC 8305 section 4).
func interleaveAddressFamilies(addresses []netip.Addr) []netip.Addr {
	if len(addresses) < 2 {
		return addresses
	}
	preferIPv4 := addresses[0].Unmap().Is4()
	var preferred, other []netip.Addr
	for _, address := range addresses {
		if address.Unmap().Is4() == preferIPv4 {
			preferred = append(preferred, address)
		} else {
			other = append(other, address)
		}
	}
	interleaved := make([]netip.Addr, 0, len(addresses))
	for index := 0; index < len(preferred) || index < len(other); index++ {
		if index < len(preferred) {
			interleaved = append(interleaved, preferred[index])
		}
		if index < len(other) {
			interleaved = append(interleaved, other[index])
		}
	}
	return interleaved
}

type dialAttempt struct {
	target     netip.AddrPort
	connection net.Conn
	err        error
}

// dialFirstToAnswer is Happy Eyeballs (RFC 8305): the first target is dialed
// at once, and each next one when the previous has failed or after
// nextAddressDelay, whichever comes first. The first connection wins; the
// others are cancelled, and any that connect anyway are closed.
func (d *streamDialer) dialFirstToAnswer(ctx context.Context, targets []netip.AddrPort) (net.Conn, error) {
	if len(targets) == 1 {
		return d.dialer.DialContext(ctx, "tcp", targets[0].String())
	}

	raceCtx, cancelRace := context.WithCancel(ctx)
	defer cancelRace()

	// Buffered for every attempt, so a loser never blocks on the send.
	attempts := make(chan dialAttempt, len(targets))
	started, running := 0, 0
	startNext := func() {
		target := targets[started]
		started++
		running++
		go func() {
			connection, err := d.dialer.DialContext(raceCtx, "tcp", target.String())
			attempts <- dialAttempt{target: target, connection: connection, err: err}
		}()
	}

	startNext()
	nextAttemptTimer := time.NewTimer(d.nextAddressDelay)
	defer nextAttemptTimer.Stop()

	var failures []error
	for running > 0 {
		select {
		case attempt := <-attempts:
			running--
			if attempt.err == nil {
				cancelRace()
				go closeLateConnections(attempts, running)
				return attempt.connection, nil
			}
			failures = append(failures, fmt.Errorf("%s: %w", attempt.target, attempt.err))
			if started < len(targets) && ctx.Err() == nil {
				startNext()
				nextAttemptTimer.Reset(d.nextAddressDelay)
			}
		case <-nextAttemptTimer.C:
			if started < len(targets) {
				startNext()
				nextAttemptTimer.Reset(d.nextAddressDelay)
			}
		}
	}
	return nil, errors.Join(failures...)
}

// closeLateConnections ends once every remaining attempt has reported, which
// the cancelled race context makes prompt.
func closeLateConnections(attempts <-chan dialAttempt, remaining int) {
	for range remaining {
		if attempt := <-attempts; attempt.connection != nil {
			attempt.connection.Close()
		}
	}
}

// Holds nothing of its own: every Stream it returns owns its socket.
func (d *streamDialer) Close() error { return nil }

// A bind address may carry port 0, meaning "any free port" -- that is how a
// SOCKS5 associate socket is opened. A zero one is refused rather than
// becoming a wildcard: an inbound that bound every interface because a field
// was omitted is a real exposure.
func bindTarget(bind spec.Endpoint) (string, error) {
	if !bind.IsValid() {
		return "", errors.New("tcp: bind address has no IP")
	}
	return netip.AddrPortFrom(bind.Addr(), bind.Port()).String(), nil
}
