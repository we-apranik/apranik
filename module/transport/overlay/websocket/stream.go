package websocket

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/shared/websocket/handshake"
	"github.com/we-apranik/apranik/spec"
)

const maxFramePayloadLength = 32 << 10

const closeFrameTimeout = time.Second

// RFC 6455 section 7.4.1.
const normalClosureStatus = 1000

var frameBuffers = sync.Pool{New: func() any {
	buffer := make([]byte, 0, maxFrameHeaderLength+maxFramePayloadLength)
	return &buffer
}}

var (
	errMaskedFromServer   = errors.New("websocket: the server sent a masked frame")
	errUnmaskedFromClient = errors.New("websocket: the client sent an unmasked frame")
)

// stream carries a byte stream in WebSocket binary frames. Message boundaries
// mean nothing to the proxy above, so every data frame, text or binary,
// fragment or not, is just more bytes.
//
// One reader and any number of writers, as spec.Stream allows. Every frame
// written takes writeMutex, but the reader never waits for it to answer a
// ping or a Close (see sendPendingControlFramesIfWriterIsIdle).
type stream struct {
	underlyingStream spec.Stream
	reader           *bufio.Reader

	isClient bool

	pendingEarlyData []byte

	remainingPayload uint64
	current          frameHeader
	maskOffset       int
	readErr          error

	pendingHandshake *clientHandshake
	handshakeLimiter *handshake.LimitedReader
	handshakeMutex   sync.Mutex
	handshakeErr     error
	handshakeDone    atomic.Bool

	handshakeFinished chan struct{}

	deadlinesMutex      sync.Mutex
	callerReadDeadline  time.Time
	callerWriteDeadline time.Time
	handshakeDeadline   time.Time

	// Client with early data: closed and replaced whenever the caller sets a
	// read deadline, so a Read waiting for the handshake picks up the new one.
	callerReadDeadlineChanged chan struct{}

	writeMutex sync.Mutex
	closeSent  bool

	// Control frames the reader owes the peer, sent by whoever next holds
	// writeMutex. Nil means none.
	pendingPongPayload atomic.Pointer[[]byte]
	pendingCloseStatus atomic.Pointer[[]byte]
}

func (s *stream) Read(p []byte) (int, error) {
	if err := s.completeHandshake(); err != nil {
		return 0, err
	}
	if len(s.pendingEarlyData) > 0 {
		n := copy(p, s.pendingEarlyData)
		s.pendingEarlyData = s.pendingEarlyData[n:]
		return n, nil
	}
	if s.readErr != nil {
		return 0, s.readErr
	}
	if len(p) == 0 {
		return 0, nil
	}

	for s.remainingPayload == 0 {
		if err := s.readFrameUntilData(); err != nil {
			s.readErr = err
			return 0, err
		}
	}

	if uint64(len(p)) > s.remainingPayload {
		p = p[:s.remainingPayload]
	}
	n, err := s.reader.Read(p)
	if s.current.masked {
		s.maskOffset = maskBytes(s.current.maskKey, s.maskOffset, p[:n])
	}
	s.remainingPayload -= uint64(n)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		s.readErr = err
	}
	return n, err
}

func (s *stream) readFrameUntilData() error {
	header, err := readFrameHeader(s.reader)
	if err != nil {
		if err == io.EOF {
			return io.EOF
		}
		return fmt.Errorf("websocket: reading a frame: %w", err)
	}
	if s.isClient && header.masked {
		return errMaskedFromServer
	}
	if !s.isClient && !header.masked {
		return errUnmaskedFromClient
	}

	if !header.isControl() {
		s.current = header
		s.remainingPayload = header.length
		s.maskOffset = 0
		return nil
	}

	var payload [maxControlPayloadLength]byte
	control := payload[:header.length]
	if _, err := io.ReadFull(s.reader, control); err != nil {
		return fmt.Errorf("websocket: reading a control frame: %w", err)
	}
	if header.masked {
		maskBytes(header.maskKey, 0, control)
	}

	switch header.opcode {
	case opcodePing:
		pongPayload := append([]byte(nil), control...)
		s.pendingPongPayload.Store(&pongPayload)
		s.sendPendingControlFramesIfWriterIsIdle()
	case opcodeClose:
		// Echo the status code, as RFC 6455 section 5.5.1 asks; a failure to
		// send it changes nothing, since the stream is ending anyway.
		status := []byte{}
		if len(control) >= 2 {
			status = append(status, control[:2]...)
		}
		s.pendingCloseStatus.Store(&status)
		s.sendPendingControlFramesIfWriterIsIdle()
		return io.EOF
	}
	return nil
}

// The whole Write goes out under one lock, as a net.Conn Write does, so frames
// from two concurrent writers never interleave.
func (s *stream) Write(p []byte) (int, error) {
	early, err := s.completeHandshakeWithEarlyData(p)
	if err != nil {
		return 0, err
	}
	s.writeMutex.Lock()
	defer s.unlockWriteAfterSendingPendingControlFrames()
	written := early
	for remaining := p[early:]; len(remaining) > 0; {
		chunk := remaining[:min(len(remaining), maxFramePayloadLength)]
		if err := s.writeFrameLocked(opcodeBinary, chunk); err != nil {
			return written, err
		}
		written += len(chunk)
		remaining = remaining[len(chunk):]
	}
	return written, nil
}

// The reader never waits for writeMutex to answer a ping or a Close: a writer
// blocked on a peer that is not reading would stop our reads too, and two
// peers doing that deadlock. So the reader leaves the frame pending and sends
// it only if the lock is free; otherwise whoever holds the lock sends it on
// the way out. Only the latest pong is kept, as RFC 6455 section 5.5.3 allows.
// A control frame that fails to send is dropped: the next read or write
// reports the broken stream.
func (s *stream) sendPendingControlFramesIfWriterIsIdle() {
	for s.hasPendingControlFrames() && s.writeMutex.TryLock() {
		s.sendPendingControlFramesLocked()
		s.writeMutex.Unlock()
	}
}

func (s *stream) hasPendingControlFrames() bool {
	return s.pendingPongPayload.Load() != nil || s.pendingCloseStatus.Load() != nil
}

func (s *stream) unlockWriteAfterSendingPendingControlFrames() {
	s.sendPendingControlFramesLocked()
	s.writeMutex.Unlock()
	s.sendPendingControlFramesIfWriterIsIdle()
}

// The Close goes last: nothing may follow it. The caller holds writeMutex.
func (s *stream) sendPendingControlFramesLocked() {
	if pongPayload := s.pendingPongPayload.Swap(nil); pongPayload != nil {
		s.writeFrameLocked(opcodePong, *pongPayload)
	}
	if closeStatus := s.pendingCloseStatus.Swap(nil); closeStatus != nil {
		s.writeFrameLocked(opcodeClose, *closeStatus)
	}
}

// writeFrameLocked writes one final frame, header and payload in a single
// write to the stream below. The caller's payload is copied, never masked in
// place. The caller holds writeMutex.
func (s *stream) writeFrameLocked(opcode byte, payload []byte) error {
	if s.closeSent {
		return net.ErrClosed
	}
	bufferPointer := frameBuffers.Get().(*[]byte)
	defer frameBuffers.Put(bufferPointer)

	header := frameHeader{final: true, opcode: opcode, masked: s.isClient, length: uint64(len(payload))}
	if s.isClient {
		rand.Read(header.maskKey[:])
	}
	frame := appendFrameHeader((*bufferPointer)[:0], header)
	payloadStart := len(frame)
	frame = append(frame, payload...)
	if s.isClient {
		maskBytes(header.maskKey, 0, frame[payloadStart:])
	}
	*bufferPointer = frame[:0]

	if opcode == opcodeClose {
		s.closeSent = true
	}
	_, err := s.underlyingStream.Write(frame)
	return err
}

// A WebSocket Close ends both directions in practice: Xray and sing-box peers
// drop the connection on it. So there is no half-close, and the relay closes
// the whole stream instead.
func (s *stream) CloseWrite() error { return spec.ErrNoHalfClose }

func (s *stream) Close() error {
	if s.handshakeDone.Load() && s.writeMutex.TryLock() {
		s.underlyingStream.SetWriteDeadline(time.Now().Add(closeFrameTimeout))
		s.writeFrameLocked(opcodeClose, binary.BigEndian.AppendUint16(nil, normalClosureStatus))
		s.writeMutex.Unlock()
	}
	// Closing first unblocks a handshake in progress, which holds the mutex.
	err := s.underlyingStream.Close()
	if !s.handshakeDone.Load() {
		s.handshakeMutex.Lock()
		s.finishHandshake(net.ErrClosed)
		s.handshakeMutex.Unlock()
	}
	return err
}

func (s *stream) LocalAddr() net.Addr  { return s.underlyingStream.LocalAddr() }
func (s *stream) RemoteAddr() net.Addr { return s.underlyingStream.RemoteAddr() }

func (s *stream) SetDeadline(t time.Time) error {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.callerReadDeadline, s.callerWriteDeadline = t, t
	s.announceCallerReadDeadlineChangedLocked()
	return s.applyDeadlinesLocked()
}

func (s *stream) SetReadDeadline(t time.Time) error {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.callerReadDeadline = t
	s.announceCallerReadDeadlineChangedLocked()
	return s.applyDeadlinesLocked()
}

func (s *stream) SetWriteDeadline(t time.Time) error {
	s.deadlinesMutex.Lock()
	defer s.deadlinesMutex.Unlock()
	s.callerWriteDeadline = t
	return s.applyDeadlinesLocked()
}

func (s *stream) announceCallerReadDeadlineChangedLocked() {
	if s.callerReadDeadlineChanged != nil {
		close(s.callerReadDeadlineChanged)
		s.callerReadDeadlineChanged = make(chan struct{})
	}
}

// Gives the stream below the caller's deadlines, or the handshake's where it
// is earlier while a deferred handshake runs. The caller holds deadlinesMutex.
func (s *stream) applyDeadlinesLocked() error {
	readErr := s.underlyingStream.SetReadDeadline(earlierDeadline(s.callerReadDeadline, s.handshakeDeadline))
	writeErr := s.underlyingStream.SetWriteDeadline(earlierDeadline(s.callerWriteDeadline, s.handshakeDeadline))
	return errors.Join(readErr, writeErr)
}
