// Package session is one client's UDP session: a SOCKS5 association, a proxied
// UDP stream, a mux sub-stream, or one source address on a plain UDP listener
// or TUN. A session may reach several outbounds, so the session opens one
// datagram per outbound, merges their replies, and closes them all together.
// It routes nothing: the caller has already chosen the outbound.
//
// An outbound datagram lives as long as the session. Its own idle behaviour,
// such as freedom's per-socket NAT timeout, belongs to the outbound.
package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/net/udp"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

// UDP has no close, so silence is the only signal a session is finished.
// PROVISIONAL: 5 minutes.
const DefaultIdleTimeout = 5 * time.Minute

var (
	ErrClosed = errors.New("session: closed")
	ErrIdle   = errors.New("session: idle timeout")

	// Send's failures that are already counted as a drop on the outbound, so
	// the caller must not count them again.
	ErrOpeningOutbound   = errors.New("session: opening outbound")
	ErrSendingToOutbound = errors.New("session: sending to outbound")
)

// Payload is valid until Release, which the receiver calls exactly once when
// it has finished with it.
type Reply struct {
	Payload []byte
	From    spec.Destination

	// The tag of the outbound whose datagram received it.
	OutboundTag string

	buffer *[]byte
	pool   *udp.BufferPool
}

// Release hands the payload's buffer back for the next reply. A receiver that
// never calls it only costs an allocation later; one that reads Payload after
// it would see another reply's bytes.
func (r Reply) Release() {
	if r.buffer != nil {
		r.pool.Release(r.buffer)
	}
}

type Config struct {
	// Identifies the session. Destination varies per packet and is not read here.
	Metadata spec.Metadata

	// Zero means DefaultIdleTimeout.
	IdleTimeout time.Duration

	// Where the session counts its datagrams, sessions and drops, on the
	// session's inbound and user and on each outbound that carried it. Nil
	// counts nothing.
	Stats *stats.Registry

	// Says why an outbound's datagram could not be created, for the outbound's
	// dial failures. Nil counts every failure as stats.DialOther.
	DialFailureReasonFor func(error) stats.DialFailureReason

	// Where reply buffers come from, which also sets the largest reply the
	// session takes: a bigger one is dropped and counted. Nil means a pool of
	// udp.DefaultMaxDatagramSize buffers shared by every session that names none.
	Buffers *udp.BufferPool

	// Nil is allowed: New substitutes a silent logger.
	Logger *logging.Logger
}

var defaultReplyBuffers = udp.NewBufferPool(udp.DefaultMaxDatagramSize)

type outboundDatagram struct {
	outboundTag string
	datagram    spec.Datagram

	// The outbound that was asked for the datagram, where this datagram's
	// drops are counted: a group counts them, not its member.
	routedOutbound *stats.OutboundCounters

	// What every datagram through it adds to, worked out once when it opened:
	// the routed outbound and, for a group, the members beneath it. Added in
	// batches from pending.
	counters []*stats.Counters
	pending  pendingDatagramCounts

	// The routed outbound's tag, then the tags beneath it that carried it.
	carryingOutboundTags []string

	// Closed on removal, to free a reply loop blocked handing a packet to the
	// session. Closing the datagram would not: it is not blocked on a read then.
	stopForwardingReplies chan struct{}
}

func (o *outboundDatagram) stopForwardingAndClose() {
	close(o.stopForwardingReplies)
	o.datagram.Close()
}

type Session struct {
	// component.Outbound.CreateDatagram takes a context, and Send has nowhere
	// else to get one.
	ctx      context.Context
	metadata spec.Metadata
	logger   *logging.Logger

	stats                *stats.Registry
	dialFailureReasonFor func(error) stats.DialFailureReason
	replyBuffers         *udp.BufferPool

	// The session's inbound and user, worked out once. Added in batches from
	// clientPending.
	clientCounters []*stats.Counters
	clientPending  pendingDatagramCounts

	idleTimeout time.Duration

	mu                             sync.Mutex
	outboundDatagramsByOutboundTag map[string]*outboundDatagram

	// Unbuffered on purpose: the kernel receive buffers are the queue, and
	// blocking a reply loop is backpressure rather than unbounded memory.
	replies chan Reply

	lastActiveUnixNano atomic.Int64
	idleTimer          *time.Timer

	done      chan struct{}
	endReason error
	closeOnce sync.Once
}

func New(ctx context.Context, config Config) *Session {
	logger := config.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	idleTimeout := config.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}

	replyBuffers := config.Buffers
	if replyBuffers == nil {
		replyBuffers = defaultReplyBuffers
	}

	newSession := &Session{
		ctx:                            ctx,
		metadata:                       config.Metadata,
		logger:                         logger,
		stats:                          config.Stats,
		dialFailureReasonFor:           config.DialFailureReasonFor,
		replyBuffers:                   replyBuffers,
		idleTimeout:                    idleTimeout,
		outboundDatagramsByOutboundTag: make(map[string]*outboundDatagram),
		replies:                        make(chan Reply),
		done:                           make(chan struct{}),
	}
	newSession.clientCounters = countersOfClient(config.Stats, config.Metadata)
	for _, counted := range newSession.clientCounters {
		counted.UDPSessionStarted()
	}
	newSession.markActive()
	newSession.idleTimer = time.AfterFunc(idleTimeout, newSession.endIfIdle)
	return newSession
}

// The session's inbound and its user once the protocol authenticated one;
// none when counting is off.
func countersOfClient(registry *stats.Registry, metadata spec.Metadata) []*stats.Counters {
	if registry == nil {
		return nil
	}
	counters := make([]*stats.Counters, 0, 2)
	if inbound := registry.Inbound(metadata.InboundTag).Base(); inbound != nil {
		counters = append(counters, inbound)
	}
	if user := registry.User(metadata.User); user != nil {
		counters = append(counters, user)
	}
	return counters
}

func (s *Session) markActive() { s.lastActiveUnixNano.Store(time.Now().UnixNano()) }

func (s *Session) hasEnded() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *Session) Replies() <-chan Reply { return s.replies }

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Err() error {
	select {
	case <-s.done:
		return s.endReason
	default:
		return nil
	}
}

func (s *Session) Send(payload []byte, to spec.Destination, via component.Outbound) error {
	if via == nil {
		return errors.New("session: no outbound to send through")
	}

	outboundDatagram, err := s.openOrReuseOutboundDatagram(via)
	if err != nil {
		return err
	}

	if _, err := outboundDatagram.datagram.WriteTo(payload, to); err != nil {
		// A write that failed because the session just ended closed the
		// datagram under it: that is the session ending, not the outbound failing.
		if s.hasEnded() {
			return ErrClosed
		}
		if errors.Is(err, component.ErrOpeningDatagramForDestination) {
			s.countOpeningFailure(via.Tag(), err)
			return fmt.Errorf("%w %q: %w", ErrOpeningOutbound, via.Tag(), err)
		}
		outboundDatagram.routedOutbound.DatagramDroppedFor(stats.OutboundDropSendFailed)
		return fmt.Errorf("%w %s through %q: %w", ErrSendingToOutbound, to, via.Tag(), err)
	}

	if len(s.clientCounters) > 0 && s.clientPending.addUplink(len(payload)) >= flushEveryDatagrams {
		s.clientPending.flushInto(s.clientCounters)
	}
	if len(outboundDatagram.counters) > 0 && outboundDatagram.pending.addUplink(len(payload)) >= flushEveryDatagrams {
		outboundDatagram.pending.flushInto(outboundDatagram.counters)
	}
	// The session may have ended and flushed while this datagram was being
	// sent: flush again, or what it just added would stay pending for good.
	if s.hasEnded() {
		s.FlushCounts()
	}
	s.markActive()
	return nil
}

// FlushCounts adds what the session has counted privately to the shared
// counters now. It is done every so many datagrams, when the session ends and
// whenever a caller asks, so a reader's numbers lag by little.
func (s *Session) FlushCounts() {
	s.clientPending.flushInto(s.clientCounters)

	s.mu.Lock()
	open := make([]*outboundDatagram, 0, len(s.outboundDatagramsByOutboundTag))
	for _, outboundDatagram := range s.outboundDatagramsByOutboundTag {
		open = append(open, outboundDatagram)
	}
	s.mu.Unlock()
	for _, outboundDatagram := range open {
		outboundDatagram.pending.flushInto(outboundDatagram.counters)
	}
}

// The tags of the outbounds that carried this session's datagram through via:
// via's own, then those beneath it when it is a group. Nil before a datagram
// was sent through via. The slice is shared; the caller must not change it.
func (s *Session) CarryingOutboundTags(via component.Outbound) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if opened, found := s.outboundDatagramsByOutboundTag[via.Tag()]; found {
		return opened.carryingOutboundTags
	}
	return nil
}

func (s *Session) openOrReuseOutboundDatagram(via component.Outbound) (*outboundDatagram, error) {
	tag := via.Tag()

	s.mu.Lock()
	if s.hasEnded() {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	if existing, found := s.outboundDatagramsByOutboundTag[tag]; found {
		s.mu.Unlock()
		return existing, nil
	}
	s.mu.Unlock()

	// Checked only here, when the outbound datagram is first opened: an
	// outbound's networks never change, and Networks() may allocate, which
	// would cost every packet.
	if !slices.Contains(via.Networks(), spec.NetworkUDP) {
		return nil, fmt.Errorf("session: outbound %q cannot carry udp: %w", tag, spec.ErrUnsupportedNetwork)
	}

	// Created outside the lock so a slow outbound does not stall sends heading
	// for a different one.
	datagram, err := via.CreateDatagram(s.ctx, s.metadata)
	if err != nil {
		s.countOpeningFailure(tag, err)
		return nil, fmt.Errorf("%w %q: %w", ErrOpeningOutbound, tag, err)
	}

	s.mu.Lock()
	if s.hasEnded() {
		s.mu.Unlock()
		datagram.Close()
		return nil, ErrClosed
	}
	if existing, found := s.outboundDatagramsByOutboundTag[tag]; found {
		s.mu.Unlock()
		datagram.Close()
		return existing, nil
	}

	carryingOutboundTags := component.TagsOfOutboundsThatCarried(via, datagram)
	outboundDatagram := &outboundDatagram{
		outboundTag:           tag,
		datagram:              datagram,
		routedOutbound:        s.stats.Outbound(tag),
		counters:              s.countersOfOutbounds(carryingOutboundTags),
		carryingOutboundTags:  carryingOutboundTags,
		stopForwardingReplies: make(chan struct{}),
	}
	s.outboundDatagramsByOutboundTag[tag] = outboundDatagram
	s.mu.Unlock()

	for _, counted := range outboundDatagram.counters {
		counted.UDPSessionStarted()
	}

	go s.forwardRepliesFrom(outboundDatagram)
	return outboundDatagram, nil
}

// An outbound that could not open a datagram, or the way to a destination for
// one, counts as a dial failure of that outbound and a drop that says so.
func (s *Session) countOpeningFailure(outboundTag string, err error) {
	routedOutbound := s.stats.Outbound(outboundTag)
	reason := stats.DialOther
	if s.dialFailureReasonFor != nil {
		reason = s.dialFailureReasonFor(err)
	}
	routedOutbound.DialFailedFor(reason)
	routedOutbound.DatagramDroppedFor(stats.OutboundDropOpenFailed)
}

func (s *Session) countersOfOutbounds(tags []string) []*stats.Counters {
	if s.stats == nil {
		return nil
	}
	counters := make([]*stats.Counters, 0, len(tags))
	for _, tag := range tags {
		if outbound := s.stats.Outbound(tag).Base(); outbound != nil {
			counters = append(counters, outbound)
		}
	}
	return counters
}

func (s *Session) forwardRepliesFrom(outboundDatagram *outboundDatagram) {
	// An outbound datagram whose reads stopForwardingReplies is removed at once, so the next
	// Send through that outbound opens a new one instead of finding a dead one.
	defer s.forgetAndCloseOutboundDatagram(outboundDatagram)

	// The last thing this datagram counts, so nothing it counted stays pending
	// once nobody will flush it.
	defer func() {
		outboundDatagram.pending.flushInto(outboundDatagram.counters)
		s.clientPending.flushInto(s.clientCounters)
	}()

	// Each reply is read straight into its own pooled buffer, which travels
	// with it to the receiver; the next read takes a fresh one.
	replyBuffer := s.replyBuffers.Acquire()
	defer func() { s.replyBuffers.Release(replyBuffer) }()
	for {
		replySize, replySender, err := outboundDatagram.datagram.ReadFrom(*replyBuffer)
		if err != nil {
			if errors.Is(err, spec.ErrMessageTooLong) {
				outboundDatagram.routedOutbound.DatagramDroppedFor(stats.OutboundDropTooLargeFromOutbound)
				s.logger.Debug("Reply dropped", "outbound", outboundDatagram.outboundTag, "error", err)
				continue
			}
			return
		}

		if len(s.clientCounters) > 0 && s.clientPending.addDownlink(replySize) >= flushEveryDatagrams {
			s.clientPending.flushInto(s.clientCounters)
		}
		if len(outboundDatagram.counters) > 0 && outboundDatagram.pending.addDownlink(replySize) >= flushEveryDatagrams {
			outboundDatagram.pending.flushInto(outboundDatagram.counters)
		}
		s.markActive()

		select {
		case s.replies <- Reply{Payload: (*replyBuffer)[:replySize], From: replySender, OutboundTag: outboundDatagram.outboundTag, buffer: replyBuffer, pool: s.replyBuffers}:
			replyBuffer = s.replyBuffers.Acquire()
		case <-outboundDatagram.stopForwardingReplies:
			outboundDatagram.routedOutbound.DatagramDroppedFor(stats.OutboundDropReplyNotDelivered)
			return
		case <-s.done:
			outboundDatagram.routedOutbound.DatagramDroppedFor(stats.OutboundDropReplyNotDelivered)
			return
		}
	}
}

// Only the caller that deletes the outbound datagram shuts it down, so a sweep
// racing a dead reply loop racing Close is safe.
func (s *Session) forgetAndCloseOutboundDatagram(outboundDatagram *outboundDatagram) {
	s.mu.Lock()
	if s.outboundDatagramsByOutboundTag[outboundDatagram.outboundTag] != outboundDatagram {
		s.mu.Unlock()
		return
	}
	delete(s.outboundDatagramsByOutboundTag, outboundDatagram.outboundTag)
	s.mu.Unlock()

	outboundDatagram.stopForwardingAndClose()
	s.logger.Debug("UDP outbound datagram released", "outbound", outboundDatagram.outboundTag)
}

// Activity only records a timestamp, so a sender never races the timer; the
// check happens here instead.
func (s *Session) endIfIdle() {
	if s.hasEnded() {
		return
	}

	if silentFor := time.Since(time.Unix(0, s.lastActiveUnixNano.Load())); silentFor < s.idleTimeout {
		s.idleTimer.Reset(s.idleTimeout - silentFor)
		return
	}

	s.logger.Debug("UDP session idle", "timeout", s.idleTimeout)
	s.endWithReason(ErrIdle)
}

func (s *Session) Close() error { return s.endWithReason(ErrClosed) }

func (s *Session) endWithReason(reason error) error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.endReason = reason
		close(s.done)
		stillOpen := make([]*outboundDatagram, 0, len(s.outboundDatagramsByOutboundTag))
		for _, outboundDatagram := range s.outboundDatagramsByOutboundTag {
			stillOpen = append(stillOpen, outboundDatagram)
		}
		s.outboundDatagramsByOutboundTag = make(map[string]*outboundDatagram)
		s.mu.Unlock()

		s.idleTimer.Stop()

		for _, outboundDatagram := range stillOpen {
			outboundDatagram.stopForwardingAndClose()
			outboundDatagram.pending.flushInto(outboundDatagram.counters)
		}
		s.clientPending.flushInto(s.clientCounters)
	})
	return nil
}
