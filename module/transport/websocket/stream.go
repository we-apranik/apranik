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

	"github.com/The-Apranik/Apranik/spec"
)

// Largest payload one Write puts in one frame. With the header it fits a
// pooled buffer, so a frame is one write to the stream below: one TLS record
// batch, not a header record and a payload record.
const maxFramePayloadLength = 32 << 10

// How long Close waits to send its Close frame before giving up on it.
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
// One reader and any number of writers, as spec.Stream allows. The reader
// answers pings and Close, so every frame written takes writeMutex.
type stream struct {
	underlyingStream spec.Stream
	reader           *bufio.Reader

	// A client masks what it writes and expects the server not to.
	isClient bool

	// Server: early data from the handshake, returned before any frame.
	pendingEarlyData []byte

	// The data frame being read: payload bytes left, and where the mask is.
	remainingPayload uint64
	current          frameHeader
	maskOffset       int
	readErr          error

	// Client with early data: the handshake waits for the first Write or Read.
	// nil once done.
	pendingHandshake *clientHandshake
	handshakeLimiter *handshakeReader
	handshakeMutex   sync.Mutex
	handshakeErr     error
	handshakeDone    atomic.Bool

	// Closed when a deferred handshake succeeds or fails.
	handshakeFinished chan struct{}

	writeMutex sync.Mutex
	closeSent  bool
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

// readFrameUntilData reads one frame header and handles it if it is a control
// frame. For a data frame it leaves remainingPayload set.
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
		if err := s.writeFrame(opcodePong, control); err != nil {
			return fmt.Errorf("websocket: answering a ping: %w", err)
		}
	case opcodeClose:
		// Echo the status code, as RFC 6455 section 5.5.1 asks; a failure to
		// send it changes nothing, since the stream is ending anyway.
		var status []byte
		if len(control) >= 2 {
			status = control[:2]
		}
		s.sendCloseOnce(status)
		return io.EOF
	}
	return nil
}

func (s *stream) Write(p []byte) (int, error) {
	early, err := s.completeHandshakeWithEarlyData(p)
	if err != nil {
		return 0, err
	}
	written := early
	for remaining := p[early:]; len(remaining) > 0; {
		chunk := remaining[:min(len(remaining), maxFramePayloadLength)]
		if err := s.writeFrame(opcodeBinary, chunk); err != nil {
			return written, err
		}
		written += len(chunk)
		remaining = remaining[len(chunk):]
	}
	return written, nil
}

// writeFrame writes one final frame, header and payload in a single write to
// the stream below. The caller's payload is copied, never masked in place.
func (s *stream) writeFrame(opcode byte, payload []byte) error {
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

	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()
	if s.closeSent {
		return net.ErrClosed
	}
	if opcode == opcodeClose {
		s.closeSent = true
	}
	_, err := s.underlyingStream.Write(frame)
	return err
}

// Nothing may follow a Close frame, and a second one is refused by
// writeFrame, so racing callers send one between them.
func (s *stream) sendCloseOnce(status []byte) {
	s.writeFrame(opcodeClose, status)
}

// A WebSocket Close ends both directions in practice: Xray and sing-box peers
// drop the connection on it. So there is no half-close, and the relay closes
// the whole stream instead.
func (s *stream) CloseWrite() error { return spec.ErrNoHalfClose }

// Sends a normal-closure Close if nothing is mid-write, then closes the
// stream below. A writer blocked on a stalled peer is not waited for.
func (s *stream) Close() error {
	if s.handshakeDone.Load() && s.writeMutex.TryLock() {
		sent := s.closeSent
		s.writeMutex.Unlock()
		if !sent {
			s.underlyingStream.SetWriteDeadline(time.Now().Add(closeFrameTimeout))
			s.sendCloseOnce(binary.BigEndian.AppendUint16(nil, normalClosureStatus))
		}
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

func (s *stream) LocalAddr() net.Addr                { return s.underlyingStream.LocalAddr() }
func (s *stream) RemoteAddr() net.Addr               { return s.underlyingStream.RemoteAddr() }
func (s *stream) SetDeadline(t time.Time) error      { return s.underlyingStream.SetDeadline(t) }
func (s *stream) SetReadDeadline(t time.Time) error  { return s.underlyingStream.SetReadDeadline(t) }
func (s *stream) SetWriteDeadline(t time.Time) error { return s.underlyingStream.SetWriteDeadline(t) }
