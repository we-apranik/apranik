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
	defaultServerIdleTimeout = 60 * time.Second

	// Holds a whole header, so a small frame costs no read of the carrier. PROVISIONAL.
	readerBufferSize = 4096

	// A stream's Write is cut into frames of this size, Xray's.
	maxStreamFrameData = 8 << 10

	maxRetainedWriteBuffer = 16 << 10

	idCount = 1 << 16
)

type mainConnection struct {
	carrier  io.ReadWriteCloser
	reader   *bufio.Reader
	isServer bool

	maxConcurrent    int
	maxTotal         int
	idleTimeout      time.Duration
	acceptedNetworks Networks

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
	pendingEnds    map[uint16]FrameOptions
	drainingEnds   bool
	acceptQueue    []*subConnection
	acceptSignal   chan struct{}

	done     chan struct{}
	loopDone chan struct{}
}

func newMainConnection(carrier io.ReadWriteCloser, isServer bool, maxConcurrent, maxTotal int, idleTimeout time.Duration, accepted Networks) *mainConnection {
	if idleTimeout == 0 {
		idleTimeout = defaultServerIdleTimeout
	}
	c := &mainConnection{
		carrier:          carrier,
		reader:           bufio.NewReaderSize(carrier, readerBufferSize),
		isServer:         isServer,
		maxConcurrent:    maxConcurrent,
		maxTotal:         maxTotal,
		idleTimeout:      idleTimeout,
		acceptedNetworks: accepted,
		writeLock:        make(chan struct{}, 1),
		subs:             make(map[uint16]*subConnection),
		nextID:           1,
		pendingEnds:      make(map[uint16]FrameOptions),
		acceptSignal:     make(chan struct{}, 1),
		done:             make(chan struct{}),
		loopDone:         make(chan struct{}),
	}
	c.mutex.Lock()
	c.startIdleTimerLocked()
	c.mutex.Unlock()
	go c.readFramesUntilClosed()
	return c
}

func closedError(cause error) error {
	if cause == nil || errors.Is(cause, ErrClosed) {
		return ErrClosed
	}
	return fmt.Errorf("%w: %w", ErrClosed, cause)
}

func (c *mainConnection) closeBecause(cause error) {
	c.mutex.Lock()
	c.closeAndUnlock(cause)
}

// closeAndUnlock: the caller holds c.mutex, released here, so a decision under
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
	subCause := endWithoutEnd(cause)
	for _, sub := range subs {
		sub.abortBecause(subCause)
	}
}

// A sub-connection still open when the main connection ends never got an End
// from the peer, so its end is not a graceful one: io.EOF is for a graceful end
func endWithoutEnd(cause error) error {
	if errors.Is(cause, io.EOF) {
		return fmt.Errorf("muxcool: the main connection ended before this sub-connection's End: %w", io.ErrUnexpectedEOF)
	}
	return cause
}

func (c *mainConnection) close() error {
	c.closeBecause(ErrClosed)
	<-c.loopDone
	return nil
}

func (c *mainConnection) activeSubConnectionsCount() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return len(c.subs)
}

// The idle clock runs while no sub-connection is open; a stale timer finds another generation.
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

func (c *mainConnection) keepAliveReceived() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if len(c.subs) == 0 {
		c.startIdleTimerLocked()
	}
}

func (c *mainConnection) subConnectionByID(id uint16) *subConnection {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.subs[id]
}

func (c *mainConnection) forgetSubConnection(sub *subConnection) {
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

func (c *mainConnection) readFramesUntilClosed() {
	defer close(c.loopDone)
	for {
		header, err := ReadHeader(c.reader)
		if err != nil {
			c.closeBecause(err)
			return
		}
		dataLength := 0
		if header.Options.Has(OptionDataFollows) {
			if dataLength, err = ReadDataLength(c.reader); err != nil {
				c.closeBecause(err)
				return
			}
		}
		if err := c.dispatchFrame(header, dataLength); err != nil {
			c.closeBecause(err)
			return
		}
	}
}

func (c *mainConnection) dispatchFrame(header Header, dataLength int) error {
	switch header.Status {
	case StatusKeepAlive:
		c.keepAliveReceived()
		return c.discard(dataLength)
	case StatusNew:
		return c.acceptNewSubConnection(header, dataLength)
	case StatusKeep:
		return c.routeKeepFrame(header, dataLength)
	case StatusEnd:
		return c.endSubConnectionFromPeer(header, dataLength)
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

// A client end ignores a New, as Xray's does.
func (c *mainConnection) acceptNewSubConnection(header Header, dataLength int) error {
	if !c.isServer {
		return c.discard(dataLength)
	}

	c.mutex.Lock()
	if c.closed {
		c.mutex.Unlock()
		return c.discard(dataLength)
	}
	if _, open := c.subs[header.SubConnectionID]; open {
		c.mutex.Unlock()
		return fmt.Errorf("%w: New for sub-connection %d, which is already open", ErrProtocol, header.SubConnectionID)
	}
	refused := !c.acceptedNetworks.accepts(header.Network) ||
		(c.maxConcurrent > 0 && len(c.subs) >= c.maxConcurrent) ||
		(c.maxTotal > 0 && c.everOpened >= c.maxTotal)
	if refused {
		c.mutex.Unlock()
		c.queueEndFrame(header.SubConnectionID, OptionError)
		return c.discard(dataLength)
	}
	sub := newSubConnection(c, header.SubConnectionID, header.Network, header.Destination, true)
	sub.globalID = header.GlobalID
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
	return sub.passDataToReader(dataLength, header)
}

func (c *mainConnection) routeKeepFrame(header Header, dataLength int) error {
	sub := c.subConnectionByID(header.SubConnectionID)
	if sub == nil {
		// The peer is sending to a sub-connection that is not open (it ended
		// here a moment ago, or never was): tell it to stop.
		if header.Options.Has(OptionDataFollows) {
			c.queueEndFrame(header.SubConnectionID, 0)
		}
		return c.discard(dataLength)
	}
	if sub.accepted && header.Destination.IsValid() {
		sub.addressInEveryDatagram.Store(true)
	}
	if dataLength == 0 {
		return nil
	}
	return sub.passDataToReader(dataLength, header)
}

func (c *mainConnection) endSubConnectionFromPeer(header Header, dataLength int) error {
	sub := c.subConnectionByID(header.SubConnectionID)
	if sub == nil {
		return c.discard(dataLength)
	}
	if dataLength > 0 {
		if err := sub.passDataToReader(dataLength, header); err != nil {
			return err
		}
	}
	c.forgetSubConnection(sub)
	sub.markEndedByPeer(header.Options.Has(OptionError))
	return nil
}

func (c *mainConnection) acquireWriteSlot(cancel, deadline <-chan struct{}) error {
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

func (c *mainConnection) releaseWriteSlot() { <-c.writeLock }

func (c *mainConnection) writeFrame(header Header, data []byte, cancel, deadline <-chan struct{}) error {
	if err := c.acquireWriteSlot(cancel, deadline); err != nil {
		return err
	}
	defer c.releaseWriteSlot()
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
	if err == nil && header.Options.Has(OptionDataFollows) {
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
		c.closeBecause(err)
		return closedError(err)
	}
	return nil
}

// queueEndFrame never blocks on the carrier; a second End for an ID replaces the first.
func (c *mainConnection) queueEndFrame(id uint16, options FrameOptions) {
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
		go c.sendQueuedEndFrames()
	}
}

func (c *mainConnection) sendQueuedEndFrames() {
	for {
		c.mutex.Lock()
		if c.closed || len(c.pendingEnds) == 0 {
			c.drainingEnds = false
			c.mutex.Unlock()
			return
		}
		var id uint16
		var options FrameOptions
		for id, options = range c.pendingEnds {
			break
		}
		delete(c.pendingEnds, id)
		c.mutex.Unlock()

		if err := c.writeFrame(Header{SubConnectionID: id, Status: StatusEnd, Options: options}, nil, nil, nil); err != nil {
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
