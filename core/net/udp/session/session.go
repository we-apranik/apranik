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

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/net/udp"
	"github.com/The-Apranik/Apranik/spec"
)

// UDP has no close, so silence is the only signal a session is finished.
// PROVISIONAL: 5 minutes.
const DefaultIdleTimeout = 5 * time.Minute

var (
	ErrClosed = errors.New("session: closed")
	ErrIdle   = errors.New("session: idle timeout")
)

// Payload is valid until Release, which the receiver calls exactly once when
// it has finished with it.
type Reply struct {
	Payload []byte
	From    spec.Destination

	buffer *[]byte
}

// Release hands the payload's buffer back for the next reply. A receiver that
// never calls it only costs an allocation later; one that reads Payload after
// it would see another reply's bytes.
func (r Reply) Release() {
	if r.buffer != nil {
		udp.ReleaseDatagramBuffer(r.buffer)
	}
}

type Config struct {
	// Identifies the session. Destination varies per packet and is not read here.
	Metadata spec.Metadata

	// Zero means DefaultIdleTimeout.
	IdleTimeout time.Duration

	// Nil is allowed: New substitutes a silent logger.
	Logger *logging.Logger
}

type outboundDatagram struct {
	outboundTag string
	datagram    spec.Datagram

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

	newSession := &Session{
		ctx:                            ctx,
		metadata:                       config.Metadata,
		logger:                         logger,
		idleTimeout:                    idleTimeout,
		outboundDatagramsByOutboundTag: make(map[string]*outboundDatagram),
		replies:                        make(chan Reply),
		done:                           make(chan struct{}),
	}
	newSession.markActive()
	newSession.idleTimer = time.AfterFunc(idleTimeout, newSession.endIfIdle)
	return newSession
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
		return fmt.Errorf("session: sending to %s through %q: %w", to, via.Tag(), err)
	}

	s.markActive()
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
		return nil, fmt.Errorf("session: opening outbound %q: %w", tag, err)
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

	outboundDatagram := &outboundDatagram{outboundTag: tag, datagram: datagram, stopForwardingReplies: make(chan struct{})}
	s.outboundDatagramsByOutboundTag[tag] = outboundDatagram
	s.mu.Unlock()

	go s.forwardRepliesFrom(outboundDatagram)
	return outboundDatagram, nil
}

func (s *Session) forwardRepliesFrom(outboundDatagram *outboundDatagram) {
	// An outbound datagram whose reads stopForwardingReplies is removed at once, so the next
	// Send through that outbound opens a new one instead of finding a dead one.
	defer s.forgetAndCloseOutboundDatagram(outboundDatagram)

	// Each reply is read straight into its own pooled buffer, which travels
	// with it to the receiver; the next read takes a fresh one.
	replyBuffer := udp.AcquireDatagramBuffer()
	defer func() { udp.ReleaseDatagramBuffer(replyBuffer) }()
	for {
		replySize, replySender, err := outboundDatagram.datagram.ReadFrom(*replyBuffer)
		if err != nil {
			if errors.Is(err, spec.ErrMessageTooLong) {
				s.logger.Debug("Reply dropped", "outbound", outboundDatagram.outboundTag, "error", err)
				continue
			}
			return
		}

		s.markActive()

		select {
		case s.replies <- Reply{Payload: (*replyBuffer)[:replySize], From: replySender, buffer: replyBuffer}:
			replyBuffer = udp.AcquireDatagramBuffer()
		case <-outboundDatagram.stopForwardingReplies:
			return
		case <-s.done:
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
		}
	})
	return nil
}
