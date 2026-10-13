// Package resumable keeps a UDP session alive after the stream that carried it
// is lost, so a client that reconnects with the same XUDP Global ID gets its
// session back: its outbound sockets, and so the source port the destination
// sees, stay the same.
//
// The relay is given a Datagram that stands in for the client's stream. While a
// stream is attached it passes datagrams through. When the stream is lost it
// waits for the hold time, and a stream with the same key that arrives in time
// is attached in its place; a stream the client ended on purpose ends the
// session at once.
package resumable

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// PROVISIONAL, as the other cores' hold: Xray keeps a session one minute.
const DefaultHoldTime = time.Minute

// Key names a client's UDP session across the streams that carry it. The user is
// in it so that another user who read the ID off an unencrypted carrier cannot
// take the session.
type Key struct {
	User     string
	GlobalID [8]byte
}

// Registry holds the sessions of one inbound. Safe for concurrent use.
type Registry struct {
	holdTime time.Duration

	mutex    sync.Mutex
	sessions map[Key]*Datagram
}

// NewRegistry: a hold time that is not positive means DefaultHoldTime.
func NewRegistry(holdTime time.Duration) *Registry {
	if holdTime <= 0 {
		holdTime = DefaultHoldTime
	}
	return &Registry{holdTime: holdTime, sessions: make(map[Key]*Datagram)}
}

// Connect says what to do with a client's stream. For a key with no session it
// returns the stand-in the session is to run on, with the stream attached
// (resumed is false). For a key that has one it attaches the stream to it, closing
// the stream it replaces if that is still open, and returns resumed true with a
// channel that is closed when this stream is no longer the session's: the caller
// holds the stream until then and has nothing else to do.
func (r *Registry) Connect(key Key, stream spec.Datagram) (standIn *Datagram, resumed bool, streamEnded <-chan struct{}) {
	for {
		r.mutex.Lock()
		existing := r.sessions[key]
		if existing == nil {
			standIn = newDatagram(r, key, stream)
			r.sessions[key] = standIn
			r.mutex.Unlock()
			return standIn, false, nil
		}
		r.mutex.Unlock()

		if ended, attached := existing.attach(stream); attached {
			return existing, true, ended
		}
		// It closed between the lookup and the attach.
		r.forget(key, existing)
	}
}

func (r *Registry) forget(key Key, session *Datagram) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.sessions[key] == session {
		delete(r.sessions, key)
	}
}

// Datagram is the client side of one UDP session, whatever stream carries it.
// Safe for one reader and many writers, which is what a relay needs.
type Datagram struct {
	registry  *Registry
	key       Key
	localAddr net.Addr

	mutex        sync.Mutex
	current      spec.Datagram // nil while no stream is attached
	currentEnded chan struct{} // closed when current is no longer the session's
	changed      chan struct{} // closed and replaced whenever the state changes
	holdTimer    *time.Timer
	closed       bool
	endReason    error // what a read returns once the session is closed
}

var _ spec.Datagram = (*Datagram)(nil)

func newDatagram(registry *Registry, key Key, first spec.Datagram) *Datagram {
	return &Datagram{
		registry:     registry,
		key:          key,
		localAddr:    first.LocalAddr(),
		current:      first,
		currentEnded: make(chan struct{}),
		changed:      make(chan struct{}),
	}
}

// signalChangeLocked wakes every reader waiting for a stream.
func (d *Datagram) signalChangeLocked() {
	close(d.changed)
	d.changed = make(chan struct{})
}

func (d *Datagram) attach(stream spec.Datagram) (streamEnded <-chan struct{}, attached bool) {
	d.mutex.Lock()
	if d.closed {
		d.mutex.Unlock()
		return nil, false
	}
	replaced := d.current
	if d.currentEnded != nil {
		close(d.currentEnded)
	}
	if d.holdTimer != nil {
		d.holdTimer.Stop()
		d.holdTimer = nil
	}
	d.current = stream
	d.currentEnded = make(chan struct{})
	ended := d.currentEnded
	d.signalChangeLocked()
	d.mutex.Unlock()

	if replaced != nil {
		replaced.Close()
	}
	return ended, true
}

// A read waits while no stream is attached. An error that ends a stream is a
// loss, which starts the hold, unless it is io.EOF: the client ended the session
// on purpose, so it ends here and now.
func (d *Datagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	for {
		d.mutex.Lock()
		current, changed, closed, endReason := d.current, d.changed, d.closed, d.endReason
		d.mutex.Unlock()
		if closed {
			return 0, spec.Destination{}, endReason
		}
		if current == nil {
			<-changed
			continue
		}

		size, from, err := current.ReadFrom(payload)
		if err == nil || errors.Is(err, spec.ErrMessageTooLong) {
			return size, from, err
		}
		if errors.Is(err, io.EOF) {
			d.closeBecause(io.EOF)
			return 0, spec.Destination{}, io.EOF
		}
		d.hold(current)
	}
}

// hold detaches a lost stream, if it is still the current one, and starts the clock.
func (d *Datagram) hold(lost spec.Datagram) {
	d.mutex.Lock()
	if d.closed || d.current != lost {
		d.mutex.Unlock()
		return
	}
	d.current = nil
	close(d.currentEnded)
	d.currentEnded = nil
	d.holdTimer = time.AfterFunc(d.registry.holdTime, d.holdRanOut)
	d.signalChangeLocked()
	d.mutex.Unlock()
	lost.Close()
}

func (d *Datagram) holdRanOut() {
	d.mutex.Lock()
	stillWaiting := !d.closed && d.current == nil
	d.mutex.Unlock()
	if stillWaiting {
		d.closeBecause(io.EOF)
	}
}

// WriteTo drops a reply while no stream is attached, and one a lost stream
// fails to carry: the read side finds the loss, and an error here would end the
// session the hold is there to keep.
func (d *Datagram) WriteTo(payload []byte, source spec.Destination) (int, error) {
	d.mutex.Lock()
	current := d.current
	d.mutex.Unlock()
	if current == nil {
		return len(payload), nil
	}
	size, err := current.WriteTo(payload, source)
	if err != nil && errors.Is(err, spec.ErrMessageTooLong) {
		return size, err
	}
	return len(payload), nil
}

// Close ends the session for good and the stream attached, if any.
func (d *Datagram) Close() error {
	d.closeBecause(net.ErrClosed)
	return nil
}

// closeBecause: reason is what a read returns from now on. The session ending
// on its own, the hold running out or the client ending it, is io.EOF, which the
// inbound does not log as a failure.
func (d *Datagram) closeBecause(reason error) {
	d.mutex.Lock()
	if d.closed {
		d.mutex.Unlock()
		return
	}
	d.closed = true
	d.endReason = reason
	current := d.current
	d.current = nil
	if d.currentEnded != nil {
		close(d.currentEnded)
		d.currentEnded = nil
	}
	if d.holdTimer != nil {
		d.holdTimer.Stop()
		d.holdTimer = nil
	}
	d.signalChangeLocked()
	d.mutex.Unlock()

	d.registry.forget(d.key, d)
	if current != nil {
		current.Close()
	}
}

func (d *Datagram) LocalAddr() net.Addr { return d.localAddr }
