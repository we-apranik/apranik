package inbound

import (
	"context"
	"errors"
	"sync"

	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

// A fixed number of places, taken without waiting: when none is free the caller
// refuses instead of queueing. A nil *slots has no limit.
type slots struct {
	taken chan struct{}
}

// Zero or less means no limit.
func newSlots(limit int) *slots {
	if limit <= 0 {
		return nil
	}
	return &slots{taken: make(chan struct{}, limit)}
}

func (s *slots) take() bool {
	if s == nil {
		return true
	}
	select {
	case s.taken <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *slots) release() {
	if s != nil {
		<-s.taken
	}
}

func (s *slots) capacity() int {
	if s == nil {
		return 0
	}
	return cap(s.taken)
}

var errUDPSessionLimitReached = errors.New("max_udp_sessions reached")

// A SOCKS5 association takes its slot when it is given its socket and gives it
// back when that socket closes, which ends the session. The proxy sees an
// ordinary failure to open a socket and answers its client accordingly.
func (i *streamInbound) associateSocketWithinLimit(openSocket spec.UDPAssociateSocketFunc) spec.UDPAssociateSocketFunc {
	if i.udpSessionSlots == nil {
		return openSocket
	}
	return func(ctx context.Context) (spec.Datagram, error) {
		if !i.udpSessionSlots.take() {
			i.refuseUDPSession()
			return nil, errUDPSessionLimitReached
		}
		socket, err := openSocket(ctx)
		if err != nil {
			i.udpSessionSlots.release()
			return nil, err
		}
		return &slotHoldingDatagram{Datagram: socket, giveBackSlot: sync.OnceFunc(i.udpSessionSlots.release)}, nil
	}
}

func (i *streamInbound) refuseUDPSession() {
	i.counters.ConnectionRefusedFor(stats.RefusalMaxUDPSessions)
	if allowed, alsoRefused := i.refusedUDPSessionLogSampler.Allow(); allowed {
		i.logger.Warn("UDP session refused: max_udp_sessions reached",
			"max_udp_sessions", i.udpSessionSlots.capacity(), "also_refused_since_last_warning", alsoRefused)
	}
}

type slotHoldingDatagram struct {
	spec.Datagram
	giveBackSlot func()
}

func (d *slotHoldingDatagram) Close() error {
	defer d.giveBackSlot()
	return d.Datagram.Close()
}
