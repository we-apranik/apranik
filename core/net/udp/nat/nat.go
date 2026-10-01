// Package nat makes UDP sent straight to the internet behave like a NAT, in
// RFC 4787 terms. A socket decides neither which local socket carries each
// destination (mapping) nor which senders may reply (filtering); Datagram
// decides both, behind the ordinary spec.Datagram interface.
//
// There is one Datagram per session: it separates destinations, not clients.
package nat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp"
	"github.com/The-Apranik/Apranik/spec"
)

// The values are the strings a config file uses, so a loader converts with
// MappingBehavior(s) and asks Valid rather than keeping a parser in sync.
type MappingBehavior string

const (
	// Default: every socket leaves from the same server IP, so separate
	// mappings per destination buy no privacy.
	EndpointIndependentMapping     MappingBehavior = "endpoint-independent"
	AddressDependentMapping        MappingBehavior = "address-dependent"
	AddressAndPortDependentMapping MappingBehavior = "address-and-port-dependent"
)

func (m MappingBehavior) Valid() bool {
	switch m {
	case EndpointIndependentMapping, AddressDependentMapping, AddressAndPortDependentMapping:
		return true
	}
	return false
}

type FilteringBehavior string

const (
	// Least safe: anyone who can reach the socket can inject packets.
	EndpointIndependentFiltering FilteringBehavior = "endpoint-independent"
	AddressDependentFiltering    FilteringBehavior = "address-dependent"

	// Default: DNS, QUIC, STUN and WireGuard all reply from the exact
	// endpoint contacted, so strictness costs them nothing.
	AddressAndPortDependentFiltering FilteringBehavior = "address-and-port-dependent"
)

func (f FilteringBehavior) Valid() bool {
	switch f {
	case EndpointIndependentFiltering, AddressDependentFiltering, AddressAndPortDependentFiltering:
		return true
	}
	return false
}

// UDP has no close, so silence is the only signal that a socket is finished.
// Independent of the session idle timeout, which is a whole-session backstop.
// PROVISIONAL: 5 minutes.
const DefaultIdleTimeout = 5 * time.Minute

// Build one with NewDefaultPolicy or NewPolicy. Fields are unexported so an
// unvalidated Policy cannot be built by hand; New rejects the zero value.
type Policy struct {
	mapping     MappingBehavior
	filtering   FilteringBehavior
	idleTimeout time.Duration
}

func NewDefaultPolicy() Policy {
	return Policy{
		mapping:     EndpointIndependentMapping,
		filtering:   AddressAndPortDependentFiltering,
		idleTimeout: DefaultIdleTimeout,
	}
}

func NewPolicy(mapping MappingBehavior, filtering FilteringBehavior, idleTimeout time.Duration) (Policy, error) {
	if !mapping.Valid() {
		return Policy{}, fmt.Errorf("nat: unknown mapping behavior %q", mapping)
	}
	if !filtering.Valid() {
		return Policy{}, fmt.Errorf("nat: unknown filtering behavior %q", filtering)
	}
	if idleTimeout <= 0 {
		return Policy{}, fmt.Errorf("nat: idle timeout must be positive, got %v", idleTimeout)
	}
	return Policy{mapping: mapping, filtering: filtering, idleTimeout: idleTimeout}, nil
}

func (p Policy) Mapping() MappingBehavior     { return p.mapping }
func (p Policy) Filtering() FilteringBehavior { return p.filtering }
func (p Policy) IdleTimeout() time.Duration   { return p.idleTimeout }

// CreateDatagramFunc opens one unconnected socket. It takes no address: the Datagram decides
// which peers each socket carries, and which local address to bind is the
// outbound's business, captured by the closure it passes in.
type CreateDatagramFunc func(ctx context.Context) (spec.Datagram, error)

type mapping struct {
	datagram spec.Datagram

	// Set when the carrier can say what it resolved a domain to. Nil for a
	// carrier that never resolves, and then domain destinations behave as they
	// did before: recorded as given, matched as given.
	resolving spec.ResolvedWriter

	mu        sync.Mutex
	contacted map[spec.Destination]struct{} // peers we have sent to; filtering reads this

	// A reply always arrives from an IP, so a domain destination needs the
	// addresses it actually went to, both to match the reply and to hand the
	// caller back the domain it asked for. Accumulated, never replaced: a
	// domain may resolve to several addresses and the answer may change.
	resolved map[netip.AddrPort]map[string]struct{}

	lastActive atomic.Int64 // unix nanoseconds of the last successful send or accepted receive
	idleTimer  *time.Timer

	// Closed on removal. Frees a readLoop blocked handing a packet to the
	// Datagram; closing datagram would not, since it is not blocked on a read then.
	stop chan struct{}
}

// Called with the Datagram lock held, so the timer cannot fire before the socket
// is published: onIdle takes that same lock.
func newMapping(datagram spec.Datagram, idleTimeout time.Duration, onIdle func(*mapping)) *mapping {
	resolving, _ := datagram.(spec.ResolvedWriter)
	created := &mapping{
		datagram:  datagram,
		resolving: resolving,
		contacted: make(map[spec.Destination]struct{}),
		resolved:  make(map[netip.AddrPort]map[string]struct{}),
		stop:      make(chan struct{}),
	}
	created.touch()
	created.idleTimer = time.AfterFunc(idleTimeout, func() { onIdle(created) })
	return created
}

func (m *mapping) touch() {
	m.lastActive.Store(time.Now().UnixNano())
}

func (m *mapping) idleFor() time.Duration {
	return time.Since(time.Unix(0, m.lastActive.Load()))
}

func (m *mapping) rememberPeer(peer spec.Destination) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contacted[peer] = struct{}{}
}

// Only a domain needs this. An IP destination is already matched exactly by
// contacted, and recording it here would let a reply from another port be
// reported under the first port we used.
func (m *mapping) rememberResolved(peer spec.Destination, sentTo spec.Endpoint) {
	if _, isDomain := peer.Domain(); !isDomain || !sentTo.IsValid() {
		return
	}

	domain, _ := peer.Domain()

	m.mu.Lock()
	defer m.mu.Unlock()

	address := netip.AddrPortFrom(sentTo.Addr(), sentTo.Port())
	if m.resolved[address] == nil {
		m.resolved[address] = make(map[string]struct{}, 1)
	}
	m.resolved[address][domain] = struct{}{}
}

// send goes through the carrier's resolved form where there is one, so the
// address a domain went to is recorded for the reply to match against.
func (m *mapping) send(payload []byte, to spec.Destination) (int, error) {
	if m.resolving == nil {
		return m.datagram.WriteTo(payload, to)
	}

	size, sentTo, err := m.resolving.WriteToResolved(payload, to)
	m.rememberResolved(to, sentTo)
	return size, err
}

// A sender is allowed if it matches a destination as written, or an address a
// domain destination actually resolved to. A spoofed sender matches neither.
func (m *mapping) allows(sender spec.Destination, filtering FilteringBehavior) bool {
	if filtering == EndpointIndependentFiltering {
		return true
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	senderIP, isIP := sender.IP()

	if filtering == AddressAndPortDependentFiltering {
		if _, ok := m.contacted[sender]; ok {
			return true
		}
		if !isIP {
			return false
		}
		_, ok := m.resolved[netip.AddrPortFrom(senderIP, sender.Port())]
		return ok
	}

	for peer := range m.contacted {
		if peer.Host() == sender.Host() {
			return true
		}
	}
	if !isIP {
		return false
	}
	for address := range m.resolved {
		if address.Addr() == senderIP {
			return true
		}
	}
	return false
}

// A client that addressed a domain expects its reply to come back from that
// domain. Matching is ordered so a shared address cannot mislabel a reply:
// a sender we contacted literally is never translated, an exact ip:port we
// resolved to wins next, and an address-only match counts only when one
// domain claims that address. Anything still ambiguous stays as the sender.
func (m *mapping) reportedSender(sender spec.Destination) spec.Destination {
	senderIP, senderIsIP := sender.IP()
	if !senderIsIP {
		return sender
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, contactedLiterally := m.contacted[sender]; contactedLiterally {
		return sender
	}

	if domains, resolvedExactly := m.resolved[netip.AddrPortFrom(senderIP, sender.Port())]; resolvedExactly {
		return onlyDomainOf(domains, sender)
	}

	domainsForAddress := make(map[string]struct{}, 1)
	for address, domains := range m.resolved {
		if address.Addr() != senderIP {
			continue
		}
		for domain := range domains {
			domainsForAddress[domain] = struct{}{}
		}
	}
	return onlyDomainOf(domainsForAddress, sender)
}

func onlyDomainOf(domains map[string]struct{}, sender spec.Destination) spec.Destination {
	if len(domains) != 1 {
		return sender
	}
	for domain := range domains {
		translated, err := spec.NewDestinationFromDomain(domain, sender.Port())
		if err != nil {
			return sender
		}
		return translated
	}
	return sender
}

func (m *mapping) shutdown() {
	m.idleTimer.Stop()
	close(m.stop)
	m.datagram.Close()
}

type mappingKey struct {
	host string
	port uint16
}

// "*" cannot come from a real host: a valid spec.Destination always carries a
// non-empty domain or IP.
var sharedMappingKey = mappingKey{host: "*"}

// A domain stays a domain here, so the same host reached by name and by
// literal IP is correctly two keys.
func mappingKeyFor(destination spec.Destination, mapping MappingBehavior) mappingKey {
	switch mapping {
	case EndpointIndependentMapping:
		return sharedMappingKey
	case AddressDependentMapping:
		return mappingKey{host: destination.Host()}
	default:
		return mappingKey{host: destination.Host(), port: destination.Port()}
	}
}

var ErrClosed = errors.New("nat: datagram is closed")

// PROVISIONAL: one rejection notice per UDP session per 10 seconds.
const rejectedLogInterval = 10 * time.Second

// buffer is pooled: ReadFrom copies payload out and releases it.
type receivedPacket struct {
	payload []byte
	buffer  *[]byte
	from    spec.Destination
}

type Datagram struct {
	// spec.Datagram's WriteTo takes no context, so a dial it triggers has
	// nowhere else to get one.
	ctx            context.Context
	createDatagram CreateDatagramFunc
	policy         Policy
	logger         *logging.Logger

	// Rejections are what a sprayer causes, so they are logged at most once
	// per interval with a count, not once per packet.
	rejectedLogSampler *logging.Sampler

	mu       sync.Mutex
	mappings map[mappingKey]*mapping

	// Unbuffered on purpose: the kernel receive buffers are the queue, and
	// blocking a readLoop is backpressure. A buffer here would trade that for
	// unbounded memory and latency.
	received  chan receivedPacket
	done      chan struct{} // closed under mu: the one record that the table is shut
	closeOnce sync.Once
}

func New(ctx context.Context, createDatagram CreateDatagramFunc, policy Policy, logger *logging.Logger) (*Datagram, error) {
	if policy == (Policy{}) {
		return nil, errors.New("nat: zero Policy; use NewDefaultPolicy or NewPolicy")
	}
	if logger == nil {
		logger = logging.Discard()
	}

	return &Datagram{
		ctx:            ctx,
		createDatagram: createDatagram,
		policy:         policy,
		logger:         logger,
		mappings:       make(map[mappingKey]*mapping),

		rejectedLogSampler: logging.NewSampler(rejectedLogInterval),
		received:           make(chan receivedPacket),
		done:               make(chan struct{}),
	}, nil
}

func (d *Datagram) closed() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

func (d *Datagram) WriteTo(payload []byte, destination spec.Destination) (int, error) {
	mapped, err := d.mappingFor(destination)
	if err != nil {
		return 0, err
	}

	mapped.rememberPeer(destination)

	size, err := mapped.send(payload, destination)
	if err != nil {
		return size, err
	}

	// Only a send that worked counts as activity; otherwise a socket whose
	// every write fails would outlive the idle timer.
	mapped.touch()
	return size, nil
}

func (d *Datagram) mappingFor(destination spec.Destination) (*mapping, error) {
	key := mappingKeyFor(destination, d.policy.mapping)

	mapped, found, err := d.lookup(key)
	if found || err != nil {
		return mapped, err
	}

	// Created outside the lock so a slow create does not stall writers to other keys.
	datagram, err := d.createDatagram(d.ctx)
	if err != nil {
		return nil, err
	}
	return d.insert(key, datagram)
}

func (d *Datagram) lookup(key mappingKey) (mapped *mapping, found bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed() {
		return nil, false, ErrClosed
	}
	mapped, found = d.mappings[key]
	return mapped, found, nil
}

func (d *Datagram) insert(key mappingKey, datagram spec.Datagram) (*mapping, error) {
	d.mu.Lock()

	if d.closed() {
		d.mu.Unlock()
		datagram.Close()
		return nil, ErrClosed
	}

	// Two writers can race to dial the same new key; the first to insert wins
	// and the other discards its socket.
	if existing, found := d.mappings[key]; found {
		d.mu.Unlock()
		datagram.Close()
		return existing, nil
	}

	mapped := newMapping(datagram, d.policy.idleTimeout, func(m *mapping) {
		d.expireIfIdle(key, m)
	})
	d.mappings[key] = mapped
	d.mu.Unlock()

	go d.readLoop(key, mapped)
	return mapped, nil
}

// A datagram longer than buffer is reported as spec.ErrMessageTooLong and
// dropped, not truncated: module/transport/base/udp does the same, and two
// implementations of one contract disagreeing about truncation is worse than
// either rule. relay.Datagram treats it as a dropped packet, not a dead
// session.
func (d *Datagram) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	select {
	case packet := <-d.received:
		defer udp.ReleaseDatagramBuffer(packet.buffer)
		if len(buffer) < len(packet.payload) {
			return 0, packet.from, fmt.Errorf("nat: %d byte datagram into a %d byte buffer: %w",
				len(packet.payload), len(buffer), spec.ErrMessageTooLong)
		}
		return copy(buffer, packet.payload), packet.from, nil
	case <-d.done:
		return 0, spec.Destination{}, ErrClosed
	}
}

func (d *Datagram) readLoop(key mappingKey, mapped *mapping) {
	defer d.remove(key, mapped)

	// Each packet is read straight into its own pooled buffer, which travels
	// with it to ReadFrom; the next read takes a fresh one. That replaces a
	// per-packet copy and allocation.
	buffer := udp.AcquireDatagramBuffer()
	defer func() { udp.ReleaseDatagramBuffer(buffer) }()
	for {
		size, sender, err := mapped.datagram.ReadFrom(*buffer)
		if err != nil {
			return
		}

		if !mapped.allows(sender, d.policy.filtering) {
			// Notice, not Debug: under the default filtering this is the
			// spoofing case the policy exists to stop.
			if allowed, alsoRejected := d.rejectedLogSampler.Allow(); allowed {
				d.logger.Notice("Inbound datagram rejected by filtering policy",
					"filtering", d.policy.filtering, "also_rejected_since_last_notice", alsoRejected)
			}
			continue
		}

		mapped.touch()

		packet := receivedPacket{payload: (*buffer)[:size], buffer: buffer, from: mapped.reportedSender(sender)}

		select {
		case d.received <- packet:
			buffer = udp.AcquireDatagramBuffer()
		case <-mapped.stop:
			return
		case <-d.done:
			return
		}
	}
}

// Runs when the idle timer fires. Activity only records a timestamp, so
// writers never race the timer; the check happens here instead.
func (d *Datagram) expireIfIdle(key mappingKey, mapped *mapping) {
	// Close has already shut this socket down and stopped its timer; rearming
	// would outlive the table.
	if d.closed() {
		return
	}

	if idle := mapped.idleFor(); idle < d.policy.idleTimeout {
		mapped.idleTimer.Reset(d.policy.idleTimeout - idle)
		return
	}
	d.remove(key, mapped)
}

// Safe to call more than once and concurrently with Close: only the caller
// that actually deletes the socket from the map shuts it down.
func (d *Datagram) remove(key mappingKey, mapped *mapping) {
	d.mu.Lock()
	if d.mappings[key] != mapped {
		d.mu.Unlock()
		return
	}
	delete(d.mappings, key)
	d.mu.Unlock()

	mapped.shutdown()
	d.logger.Debug("Mapping reaped", "mapping", d.policy.mapping)
}

func (d *Datagram) Close() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		close(d.done)
		mappings := d.mappings
		d.mappings = make(map[mappingKey]*mapping)
		d.mu.Unlock()

		for _, mapped := range mappings {
			mapped.shutdown()
		}
	})
	return nil
}

// Ambiguous when the mapping allows more than one live socket: returns any
// one of them, or nil before the first send.
func (d *Datagram) LocalAddr() net.Addr {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, mapped := range d.mappings {
		return mapped.datagram.LocalAddr()
	}
	return nil
}
