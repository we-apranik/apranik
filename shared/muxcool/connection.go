package muxcool

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	defaultClientIdleTimeout = 15 * time.Second
	defaultServerIdleTimeout = 60 * time.Second

	// The reader holds a whole header (514 bytes at most) and spares a read of
	// the carrier for each small frame. One per main connection, none per
	// sub-connection. PROVISIONAL.
	readerBufferSize = 4096

	// A stream's Write is cut into frames of this size, Xray's.
	maxStreamFrameData = 8 << 10

	// A write buffer that grew past this for one big datagram is dropped, not
	// kept for the life of the main connection.
	maxRetainedWriteBuffer = 16 << 10

	idCount = 1 << 16
)

// mainConnection is one established reliable stream carrying sub-connections,
// the engine both Client and Server are built on. One goroutine reads the
// carrier and hands each frame's data straight to the sub-connection that reads
// it; writes are made by the callers, one whole frame at a time.
type mainConnection struct {
	carrier  io.ReadWriteCloser
	reader   *bufio.Reader
	isServer bool

	maxConcurrent    int
	maxEver          int
	idleTimeout      time.Duration
	acceptedNetworks Networks

	// A one-slot channel instead of a mutex, so a waiter can give up on a write
	// deadline or when its sub-connection closes.
	writeLock   chan struct{}
	writeBuffer []byte // guarded by writeLock

	mutex          sync.Mutex
	subs           map[uint16]*subConnection
	everOpened     int
	nextID         int
	closed         bool
	closeCause     error
	idleTimer      *time.Timer
	idleGeneration uint64
	pendingEnds    map[uint16]Options
	drainingEnds   bool
	acceptQueue    []*subConnection
	acceptSignal   chan struct{}

	done     chan struct{}
	loopDone chan struct{}
}

func newMainConnection(carrier io.ReadWriteCloser, isServer bool, maxConcurrent, maxEver int, idleTimeout time.Duration, accepted Networks) *mainConnection {
	if idleTimeout == 0 {
		idleTimeout = defaultClientIdleTimeout
		if isServer {
			idleTimeout = defaultServerIdleTimeout
		}
	}
	c := &mainConnection{
		carrier:          carrier,
		reader:           bufio.NewReaderSize(carrier, readerBufferSize),
		isServer:         isServer,
		maxConcurrent:    maxConcurrent,
		maxEver:          maxEver,
		idleTimeout:      idleTimeout,
		acceptedNetworks: accepted,
		writeLock:        make(chan struct{}, 1),
		subs:             make(map[uint16]*subConnection),
		nextID:           1,
		pendingEnds:      make(map[uint16]Options),
		acceptSignal:     make(chan struct{}, 1),
		done:             make(chan struct{}),
		loopDone:         make(chan struct{}),
	}
	c.mutex.Lock()
	c.startIdleTimerLocked()
	c.mutex.Unlock()
	go c.readLoop()
	return c
}

// closedError is what an operation on a closed connection returns: ErrClosed,
// and the reason when there is one.
func closedError(cause error) error {
	if cause == nil || errors.Is(cause, ErrClosed) {
		return ErrClosed
	}
	return fmt.Errorf("%w: %w", ErrClosed, cause)
}

func (c *mainConnection) closeWith(cause error) {
	c.mutex.Lock()
	c.closeAndUnlock(cause)
}

// closeAndUnlock closes the main connection. The caller holds c.mutex, which is
// released here, so a decision made under the lock (the idle clock found no
// sub-connection) and the closing cannot be separated by an open.
func (c *mainConnection) closeAndUnlock(cause error) {
	if c.closed {
		c.mutex.Unlock()
		return
	}
	c.closed = true
	c.closeCause = cause
	c.idleGeneration++
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	subs := make([]*subConnection, 0, len(c.subs))
	for _, sub := range c.subs {
		subs = append(subs, sub)
	}
	c.subs = make(map[uint16]*subConnection)
	c.acceptQueue = nil
	c.mutex.Unlock()

	close(c.done)
	_ = c.carrier.Close()
	for _, sub := range subs {
		sub.abort(cause)
	}
}

// close ends the main connection and waits for its reader to stop.
func (c *mainConnection) close() error {
	c.closeWith(ErrClosed)
	<-c.loopDone
	return nil
}

func (c *mainConnection) active() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return len(c.subs)
}

// full says whether another sub-connection can be opened here. The caller
// holds c.mutex.
func (c *mainConnection) fullLocked() bool {
	return c.closed ||
		c.nextID >= idCount ||
		(c.maxConcurrent > 0 && len(c.subs) >= c.maxConcurrent) ||
		(c.maxEver > 0 && c.everOpened >= c.maxEver)
}

func (c *mainConnection) full() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.fullLocked()
}

// The idle clock runs while no sub-connection is open. A timer firing late for
// a clock that was stopped or restarted since finds another generation and does
// nothing.
func (c *mainConnection) startIdleTimerLocked() {
	if c.idleTimeout < 0 || c.closed {
		return
	}
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	c.idleGeneration++
	generation := c.idleGeneration
	c.idleTimer = time.AfterFunc(c.idleTimeout, func() { c.idleFired(generation) })
}

func (c *mainConnection) stopIdleTimerLocked() {
	c.idleGeneration++
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
}

func (c *mainConnection) idleFired(generation uint64) {
	c.mutex.Lock()
	if c.closed || generation != c.idleGeneration || len(c.subs) != 0 {
		c.mutex.Unlock()
		return
	}
	c.closeAndUnlock(fmt.Errorf("%w: no sub-connection for %s", ErrClosed, c.idleTimeout))
}

// A KeepAlive frame keeps the main connection from being closed as idle, which
// is its whole purpose in the specification.
func (c *mainConnection) keepAliveReceived() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if len(c.subs) == 0 {
		c.startIdleTimerLocked()
	}
}

func (c *mainConnection) lookup(id uint16) *subConnection {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.subs[id]
}

// remove takes a sub-connection out of the table, if it is still the one there,
// and starts the idle clock when it was the last.
func (c *mainConnection) remove(sub *subConnection) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.subs[sub.id] != sub {
		return
	}
	delete(c.subs, sub.id)
	if len(c.subs) == 0 {
		c.startIdleTimerLocked()
	}
}

// ---- reading

func (c *mainConnection) readLoop() {
	defer close(c.loopDone)
	for {
		header, err := ReadHeader(c.reader)
		if err != nil {
			c.closeWith(err)
			return
		}
		dataLength := 0
		if header.Options.Has(OptionData) {
			if dataLength, err = ReadDataLength(c.reader); err != nil {
				c.closeWith(err)
				return
			}
		}
		if err := c.handleFrame(header, dataLength); err != nil {
			c.closeWith(err)
			return
		}
	}
}

func (c *mainConnection) handleFrame(header Header, dataLength int) error {
	switch header.Status {
	case StatusKeepAlive:
		c.keepAliveReceived()
		return c.discard(dataLength)
	case StatusNew:
		return c.handleNew(header, dataLength)
	case StatusKeep:
		return c.handleKeep(header, dataLength)
	case StatusEnd:
		return c.handleEnd(header, dataLength)
	default:
		return fmt.Errorf("%w: frame status %s", ErrProtocol, header.Status)
	}
}

func (c *mainConnection) discard(length int) error {
	if length == 0 {
		return nil
	}
	if _, err := c.reader.Discard(length); err != nil {
		return fmt.Errorf("muxcool: reading frame data: %w", unexpectedIfEnded(err))
	}
	return nil
}

// A server end registers the sub-connection the peer asks for. A client end
// ignores a New, whose data it discards: only a server end is never sent one by
// a correct peer, and Xray's client ignores it as well.
func (c *mainConnection) handleNew(header Header, dataLength int) error {
	if !c.isServer {
		return c.discard(dataLength)
	}

	c.mutex.Lock()
	if c.closed {
		c.mutex.Unlock()
		return c.discard(dataLength)
	}
	if _, open := c.subs[header.SessionID]; open {
		c.mutex.Unlock()
		return fmt.Errorf("%w: New for sub-connection %d, which is already open", ErrProtocol, header.SessionID)
	}
	refused := !c.acceptedNetworks.accepts(header.Network) ||
		(c.maxConcurrent > 0 && len(c.subs) >= c.maxConcurrent) ||
		(c.maxEver > 0 && c.everOpened >= c.maxEver)
	if refused {
		c.mutex.Unlock()
		c.queueEnd(header.SessionID, OptionError)
		return c.discard(dataLength)
	}
	sub := newSubConnection(c, header.SessionID, header.Network, header.Destination, true)
	c.subs[sub.id] = sub
	c.everOpened++
	c.stopIdleTimerLocked()
	c.acceptQueue = append(c.acceptQueue, sub)
	c.mutex.Unlock()

	select {
	case c.acceptSignal <- struct{}{}:
	default:
	}
	if dataLength == 0 {
		return nil
	}
	return sub.deliver(dataLength, header)
}

func (c *mainConnection) handleKeep(header Header, dataLength int) error {
	sub := c.lookup(header.SessionID)
	if sub == nil {
		// The peer is sending to a sub-connection that is not open (it ended
		// here a moment ago, or never was): tell it to stop.
		if header.Options.Has(OptionData) {
			c.queueEnd(header.SessionID, 0)
		}
		return c.discard(dataLength)
	}
	if dataLength == 0 {
		return nil
	}
	return sub.deliver(dataLength, header)
}

// An End may carry data (the specification says it must be delivered), so that
// is delivered first, then the sub-connection ends.
func (c *mainConnection) handleEnd(header Header, dataLength int) error {
	sub := c.lookup(header.SessionID)
	if sub == nil {
		return c.discard(dataLength)
	}
	if dataLength > 0 {
		if err := sub.deliver(dataLength, header); err != nil {
			return err
		}
	}
	c.remove(sub)
	sub.remoteEnded(header.Options.Has(OptionError))
	return nil
}

// ---- writing

// lockWrite waits for the right to write one frame. It gives up when the main
// connection closes, when cancel is closed (the sub-connection ended) or when
// deadline passes.
func (c *mainConnection) lockWrite(cancel, deadline <-chan struct{}) error {
	select {
	case c.writeLock <- struct{}{}:
		return nil
	default:
	}
	select {
	case c.writeLock <- struct{}{}:
		return nil
	case <-c.done:
		return closedError(c.cause())
	case <-cancel:
		return ErrClosed
	case <-deadline:
		return os.ErrDeadlineExceeded
	}
}

func (c *mainConnection) cause() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.closeCause
}

func (c *mainConnection) unlockWrite() { <-c.writeLock }

// writeFrame writes one whole frame, header and data together, with one Write
// of the carrier. A failed write closes the main connection: the carrier may
// hold half a frame.
func (c *mainConnection) writeFrame(header Header, data []byte, cancel, deadline <-chan struct{}) error {
	if err := c.lockWrite(cancel, deadline); err != nil {
		return err
	}
	defer c.unlockWrite()
	if isClosed(c.done) {
		return closedError(c.cause())
	}
	if isClosed(cancel) {
		return ErrClosed
	}
	return c.writeFrameLocked(header, data)
}

func (c *mainConnection) writeFrameLocked(header Header, data []byte) error {
	buffer, err := AppendHeader(c.writeBuffer[:0], header)
	if err == nil && header.Options.Has(OptionData) {
		if buffer, err = AppendDataLength(buffer, len(data)); err == nil {
			buffer = append(buffer, data...)
		}
	}
	if err != nil {
		return err
	}
	if cap(buffer) > maxRetainedWriteBuffer {
		c.writeBuffer = nil
	} else {
		c.writeBuffer = buffer
	}
	if _, err := c.carrier.Write(buffer); err != nil {
		c.closeWith(err)
		return closedError(err)
	}
	return nil
}

// queueEnd asks for an End frame without waiting for the carrier, so that a
// close, or the reader answering a frame, never blocks on a peer that is not
// reading. One goroutine at a time sends what is queued; a second End for an ID
// already waiting replaces the first.
func (c *mainConnection) queueEnd(id uint16, options Options) {
	c.mutex.Lock()
	if c.closed {
		c.mutex.Unlock()
		return
	}
	c.pendingEnds[id] |= options
	start := !c.drainingEnds
	c.drainingEnds = true
	c.mutex.Unlock()
	if start {
		go c.drainEnds()
	}
}

func (c *mainConnection) drainEnds() {
	for {
		c.mutex.Lock()
		if c.closed || len(c.pendingEnds) == 0 {
			c.drainingEnds = false
			c.mutex.Unlock()
			return
		}
		var id uint16
		var options Options
		for id, options = range c.pendingEnds {
			break
		}
		delete(c.pendingEnds, id)
		c.mutex.Unlock()

		if err := c.writeFrame(Header{SessionID: id, Status: StatusEnd, Options: options}, nil, nil, nil); err != nil {
			c.mutex.Lock()
			c.drainingEnds = false
			c.mutex.Unlock()
			return
		}
	}
}

func (c *mainConnection) sendKeepAlive() error {
	return c.writeFrame(Header{Status: StatusKeepAlive}, nil, nil, nil)
}
