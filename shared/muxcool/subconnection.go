package muxcool

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/we-apranik/apranik/spec"
)

type subState int

const (
	stateOpen subState = iota
	stateClosedLocal
	stateRemoteEnded
	stateAborted
)

type subConnection struct {
	conn        *mainConnection
	id          uint16
	network     Network
	destination spec.Destination
	accepted    bool // this end is the server end: it never sends New

	globalID [8]byte

	// addressInEveryDatagram is XUDP: set at open (client) or when the peer first names an address (server).
	addressInEveryDatagram atomic.Bool

	readDeadline  deadline
	writeDeadline deadline

	closedCh     chan struct{}
	ready        chan struct{}
	deliveryDone chan struct{}

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

// passDataToReader blocks until length bytes of data, which are next in the carrier,
// have been read by this sub-connection's reader, or dropped because it ended.
func (s *subConnection) passDataToReader(length int, header Header) error {
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
		s.mutex.Unlock()
	}
}

func (s *subConnection) claimDelivery(deadline <-chan struct{}) (remaining int, source spec.Destination, err error) {
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

func (s *subConnection) finishDelivery(consumed int, readErr error) {
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
		s.conn.closeBecause(fmt.Errorf("muxcool: reading frame data: %w", unexpectedIfEnded(readErr)))
		signal = true
	}
	if signal {
		select {
		case s.deliveryDone <- struct{}{}:
		default:
		}
	}
}

func (s *subConnection) close() error {
	s.mutex.Lock()
	if s.state != stateOpen {
		s.mutex.Unlock()
		return nil
	}
	s.state = stateClosedLocal
	close(s.closedCh)
	s.mutex.Unlock()

	s.conn.forgetSubConnection(s)
	if s.peerKnows.Load() {
		s.conn.queueEndFrame(s.id, 0)
	}
	return nil
}

func (s *subConnection) markEndedByPeer(reset bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state != stateOpen {
		return
	}
	s.state = stateRemoteEnded
	s.remoteReset = reset
	close(s.closedCh)
}

func (s *subConnection) abortBecause(cause error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state != stateOpen {
		return
	}
	s.state = stateAborted
	s.abortCause = cause
	close(s.closedCh)
}

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

func (s *subConnection) writeKeep(data []byte) error {
	err := s.conn.writeFrame(Header{SubConnectionID: s.id, Status: StatusKeep, Options: OptionDataFollows}, data, s.closedCh, s.writeDeadline.wait())
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return err
		}
		return s.writeError(err)
	}
	return nil
}

func (s *subConnection) writeDatagram(data []byte, to spec.Destination) error {
	cancel, deadline := s.closedCh, s.writeDeadline.wait()
	if err := s.conn.acquireWriteSlot(cancel, deadline); err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return err
		}
		return s.writeError(err)
	}
	defer s.conn.releaseWriteSlot()

	namesAddress := s.addressInEveryDatagram.Load()
	header := Header{SubConnectionID: s.id, Status: StatusKeep, Options: OptionDataFollows}
	if namesAddress {
		header.Network = NetworkUDP
		header.Destination = to
	}
	if !s.newSent {
		header.Status = StatusNew
		header.Network = NetworkUDP
		header.GlobalID = s.globalID
		if !namesAddress {
			header.Destination = s.destination
		}
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
