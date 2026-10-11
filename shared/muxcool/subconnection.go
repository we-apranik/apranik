package muxcool

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/The-Apranik/Apranik/spec"
)

type subState int

const (
	stateOpen subState = iota
	stateClosedLocal
	stateRemoteEnded
	stateAborted
)

// subConnection is one conversation on a main connection. It holds no buffer:
// the reader of the main connection waits for this side to read, then the
// payload goes from the carrier into the caller's slice (a "delivery"). So a
// sub-connection that nobody reads holds up its whole main connection, which is
// the cost of a format without flow control, and it costs no memory.
type subConnection struct {
	conn        *mainConnection
	id          uint16
	network     Network
	destination spec.Destination
	accepted    bool // this end is the server end: it never sends New

	readDeadline  deadline
	writeDeadline deadline

	closedCh     chan struct{} // closed when the sub-connection reaches any end state
	ready        chan struct{} // a delivery is waiting for a reader; a hint, may be stale
	deliveryDone chan struct{} // the reader finished a delivery, or the sub-connection ended while one was claimed

	// Whether the peer has heard of this sub-connection, so a Close knows if an
	// End is owed. Set before the New frame is written, so it can only be too
	// generous, and an End for an ID the peer never opened is ignored.
	peerKnows atomic.Bool

	// Guarded by conn.writeLock.
	newSent bool

	mutex             sync.Mutex
	state             subState
	remoteReset       bool
	abortCause        error
	delivering        bool
	deliveryRemaining int
	deliveryClaimed   bool
	deliverySource    spec.Destination
}

func newSubConnection(conn *mainConnection, id uint16, network Network, destination spec.Destination, accepted bool) *subConnection {
	s := &subConnection{
		conn:          conn,
		id:            id,
		network:       network,
		destination:   destination,
		accepted:      accepted,
		readDeadline:  newDeadline(),
		writeDeadline: newDeadline(),
		closedCh:      make(chan struct{}),
		ready:         make(chan struct{}, 1),
		deliveryDone:  make(chan struct{}, 1),
		newSent:       accepted,
	}
	if accepted {
		s.peerKnows.Store(true)
	}
	return s
}

// ---- the reader of the main connection hands data over

// deliver blocks until length bytes of data, which are next in the carrier,
// have been read by this sub-connection's reader, or dropped because it ended.
// The returned error is only for the carrier failing.
func (s *subConnection) deliver(length int, header Header) error {
	var source spec.Destination
	if header.Network == NetworkUDP {
		source = header.Destination
	}

	s.mutex.Lock()
	if s.state != stateOpen {
		s.mutex.Unlock()
		return s.conn.discard(length)
	}
	s.delivering = true
	s.deliveryRemaining = length
	s.deliveryClaimed = false
	s.deliverySource = source
	s.mutex.Unlock()

	select {
	case s.ready <- struct{}{}:
	default:
	}

	for {
		select {
		case <-s.deliveryDone:
		case <-s.closedCh:
		}

		s.mutex.Lock()
		if !s.delivering {
			s.mutex.Unlock()
			return nil
		}
		if s.deliveryClaimed {
			// A reader is in the middle of the data: its token comes when it
			// is done, and closedCh must not be waited on again meanwhile.
			s.mutex.Unlock()
			<-s.deliveryDone
			continue
		}
		if s.state != stateOpen {
			remaining := s.deliveryRemaining
			s.delivering = false
			s.mutex.Unlock()
			return s.conn.discard(remaining)
		}
		s.mutex.Unlock() // a partial read; the rest is still waiting for a reader
	}
}

// awaitDelivery waits until a delivery is ready for this reader and claims it,
// or the sub-connection ended, or the deadline passed.
func (s *subConnection) awaitDelivery(deadline <-chan struct{}) (remaining int, source spec.Destination, err error) {
	for {
		s.mutex.Lock()
		switch s.state {
		case stateClosedLocal:
			s.mutex.Unlock()
			return 0, spec.Destination{}, ErrClosed
		case stateAborted:
			cause := s.abortCause
			s.mutex.Unlock()
			return 0, spec.Destination{}, closedError(cause)
		case stateRemoteEnded:
			reset := s.remoteReset
			s.mutex.Unlock()
			if reset {
				return 0, spec.Destination{}, ErrRemoteReset
			}
			return 0, spec.Destination{}, io.EOF
		}
		if isClosed(deadline) {
			s.mutex.Unlock()
			return 0, spec.Destination{}, os.ErrDeadlineExceeded
		}
		if s.delivering && !s.deliveryClaimed {
			s.deliveryClaimed = true
			remaining, source = s.deliveryRemaining, s.deliverySource
			s.mutex.Unlock()
			return remaining, source, nil
		}
		s.mutex.Unlock()

		select {
		case <-s.ready:
		case <-s.closedCh:
		case <-deadline:
			return 0, spec.Destination{}, os.ErrDeadlineExceeded
		}
	}
}

// finishRead gives the claimed delivery back after consuming bytes of it. A
// failure of the carrier ends the main connection.
func (s *subConnection) finishRead(consumed int, readErr error) {
	s.mutex.Lock()
	s.deliveryRemaining -= consumed
	s.deliveryClaimed = false
	signal := false
	if s.deliveryRemaining <= 0 {
		s.delivering = false
		signal = true
	} else if s.state != stateOpen {
		signal = true
	}
	s.mutex.Unlock()

	if readErr != nil {
		s.conn.closeWith(fmt.Errorf("muxcool: reading frame data: %w", unexpectedIfEnded(readErr)))
		signal = true
	}
	if signal {
		select {
		case s.deliveryDone <- struct{}{}:
		default:
		}
	}
}

// ---- how a sub-connection ends

func (s *subConnection) close() error {
	s.mutex.Lock()
	if s.state != stateOpen {
		s.mutex.Unlock()
		return nil
	}
	s.state = stateClosedLocal
	close(s.closedCh)
	s.mutex.Unlock()

	s.conn.remove(s)
	if s.peerKnows.Load() {
		s.conn.queueEnd(s.id, 0)
	}
	return nil
}

func (s *subConnection) remoteEnded(reset bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state != stateOpen {
		return
	}
	s.state = stateRemoteEnded
	s.remoteReset = reset
	close(s.closedCh)
}

func (s *subConnection) abort(cause error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state != stateOpen {
		return
	}
	s.state = stateAborted
	s.abortCause = cause
	close(s.closedCh)
}

// writeError says what a write that could not happen returns, by how the
// sub-connection stands.
func (s *subConnection) writeError(err error) error {
	s.mutex.Lock()
	state, cause := s.state, s.abortCause
	s.mutex.Unlock()
	switch state {
	case stateClosedLocal:
		return ErrClosed
	case stateRemoteEnded:
		return io.ErrClosedPipe
	case stateAborted:
		return closedError(cause)
	}
	return err
}

func (s *subConnection) writable() error {
	return s.writeError(nil)
}

// ---- writing

func (s *subConnection) writeKeep(data []byte) error {
	err := s.conn.writeFrame(Header{SessionID: s.id, Status: StatusKeep, Options: OptionData}, data, s.closedCh, s.writeDeadline.wait())
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return err
		}
		return s.writeError(err)
	}
	return nil
}

// writeDatagram writes one datagram of a UDP sub-connection. The first one a
// client writes is carried by the New frame that opens the sub-connection.
func (s *subConnection) writeDatagram(data []byte) error {
	cancel, deadline := s.closedCh, s.writeDeadline.wait()
	if err := s.conn.lockWrite(cancel, deadline); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return err
		}
		return s.writeError(err)
	}
	defer s.conn.unlockWrite()

	header := Header{SessionID: s.id, Status: StatusKeep, Options: OptionData}
	if !s.newSent {
		header.Status = StatusNew
		header.Network = NetworkUDP
		header.Destination = s.destination
		s.peerKnows.Store(true)
	}
	if isClosed(s.conn.done) {
		return closedError(s.conn.cause())
	}
	if isClosed(cancel) {
		return s.writeError(ErrClosed)
	}
	if err := s.conn.writeFrameLocked(header, data); err != nil {
		return err
	}
	s.newSent = true
	return nil
}
