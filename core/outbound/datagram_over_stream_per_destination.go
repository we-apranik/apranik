package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/spec"
)

// datagramOverStreamPerDestination is for proxy protocols that write the
// datagram destination in the stream request: it opens a new stream to the
// server the first time a destination is used, and keeps one datagram per
// destination for the rest of the UDP session. To the session it is one
// datagram that can be sent to any destination.
//
// A destination's datagram that dies is forgotten alone; the next write to
// that destination opens a new one.
type datagramOverStreamPerDestination struct {
	outbound       *streamOutbound
	sessionContext context.Context
	metadata       spec.Metadata

	mutex                 sync.Mutex
	datagramByDestination map[spec.Destination]spec.Datagram
	closed                bool

	// The first ReadFrom says how big a reply the session takes, by the size
	// of its buffer; replies are read from the datagrams into buffers of that
	// size and handed over one at a time.
	learnReplyBufferSize sync.Once
	replyBufferSize      int
	replyBufferSizeKnown chan struct{}

	replies chan replyFromDestination
	ended   chan struct{}
}

type replyFromDestination struct {
	payload []byte
	from    spec.Destination

	// A reply that could not be read whole, such as one too large; nothing else is set.
	err error

	// Told once the payload has been copied out, so its datagram may read
	// into the same buffer again.
	copied chan<- struct{}
}

func newDatagramOverStreamPerDestination(sessionContext context.Context, outbound *streamOutbound, metadata spec.Metadata) *datagramOverStreamPerDestination {
	return &datagramOverStreamPerDestination{
		outbound:              outbound,
		sessionContext:        sessionContext,
		metadata:              metadata,
		datagramByDestination: make(map[spec.Destination]spec.Datagram),
		replyBufferSizeKnown:  make(chan struct{}),
		replies:               make(chan replyFromDestination),
		ended:                 make(chan struct{}),
	}
}

// Dialing waits for the server here, so the first datagram to a new destination
// is delayed by it, and so is whatever else the session sends meanwhile.
func (d *datagramOverStreamPerDestination) WriteTo(payload []byte, destination spec.Destination) (int, error) {
	datagram, err := d.datagramFor(destination)
	if err != nil {
		return 0, err
	}
	written, err := datagram.WriteTo(payload, destination)
	if err != nil {
		d.forgetAndClose(destination, datagram)
	}
	return written, err
}

func (d *datagramOverStreamPerDestination) datagramFor(destination spec.Destination) (spec.Datagram, error) {
	d.mutex.Lock()
	if d.closed {
		d.mutex.Unlock()
		return nil, net.ErrClosed
	}
	if existing, found := d.datagramByDestination[destination]; found {
		d.mutex.Unlock()
		return existing, nil
	}
	d.mutex.Unlock()

	// Opened outside the lock so a slow server does not stall Close or the
	// reply loops.
	opened, err := d.openDatagramFor(destination)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", component.ErrOpeningDatagramForDestination, err)
	}

	d.mutex.Lock()
	if d.closed {
		d.mutex.Unlock()
		opened.Close()
		return nil, net.ErrClosed
	}
	if existing, found := d.datagramByDestination[destination]; found {
		d.mutex.Unlock()
		opened.Close()
		return existing, nil
	}
	d.datagramByDestination[destination] = opened
	d.mutex.Unlock()

	go d.forwardRepliesFrom(destination, opened)
	return opened, nil
}

func (d *datagramOverStreamPerDestination) openDatagramFor(destination spec.Destination) (spec.Datagram, error) {
	serverStream, err := d.outbound.dialServer(d.sessionContext)
	if err != nil {
		return nil, err
	}
	metadata := d.metadata
	metadata.Destination = destination
	datagram, err := d.outbound.datagramClient.RequestDatagramOverStream(d.sessionContext, serverStream, metadata)
	if err != nil {
		serverStream.Close()
		d.outbound.logger.Debug("Proxy handshake failed", "error", err)
		return nil, fmt.Errorf("outbound %q: %w", d.outbound.tag, err)
	}
	return datagram, nil
}

func (d *datagramOverStreamPerDestination) forwardRepliesFrom(destination spec.Destination, datagram spec.Datagram) {
	defer d.forgetAndClose(destination, datagram)

	select {
	case <-d.replyBufferSizeKnown:
	case <-d.ended:
		return
	}
	buffer := make([]byte, d.replyBufferSize)
	copied := make(chan struct{}, 1)

	for {
		size, from, err := datagram.ReadFrom(buffer)
		reply := replyFromDestination{payload: buffer[:size], from: from, copied: copied}
		if err != nil {
			if !errors.Is(err, spec.ErrMessageTooLong) {
				return
			}
			reply = replyFromDestination{err: err}
		}

		select {
		case d.replies <- reply:
		case <-d.ended:
			return
		}
		if reply.err != nil {
			continue
		}
		select {
		case <-copied:
		case <-d.ended:
			return
		}
	}
}

func (d *datagramOverStreamPerDestination) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	d.learnReplyBufferSize.Do(func() {
		d.replyBufferSize = len(buffer)
		close(d.replyBufferSizeKnown)
	})

	select {
	case reply := <-d.replies:
		if reply.err != nil {
			return 0, spec.Destination{}, reply.err
		}
		size := copy(buffer, reply.payload)
		tooLong := size < len(reply.payload)
		reply.copied <- struct{}{}
		if tooLong {
			return 0, spec.Destination{}, fmt.Errorf("outbound %q: %d byte reply into a %d byte buffer: %w", d.outbound.tag, len(reply.payload), len(buffer), spec.ErrMessageTooLong)
		}
		return size, reply.from, nil
	case <-d.ended:
		return 0, spec.Destination{}, net.ErrClosed
	}
}

// Only the caller that removes a datagram closes it, so a write that failed
// racing its dead reply loop racing Close is safe.
func (d *datagramOverStreamPerDestination) forgetAndClose(destination spec.Destination, datagram spec.Datagram) {
	d.mutex.Lock()
	if d.datagramByDestination[destination] != datagram {
		d.mutex.Unlock()
		return
	}
	delete(d.datagramByDestination, destination)
	d.mutex.Unlock()

	datagram.Close()
	d.outbound.logger.Debug("UDP destination released", "destination", destination.String())
}

func (d *datagramOverStreamPerDestination) Close() error {
	d.mutex.Lock()
	if d.closed {
		d.mutex.Unlock()
		return nil
	}
	d.closed = true
	stillOpen := make([]spec.Datagram, 0, len(d.datagramByDestination))
	for _, datagram := range d.datagramByDestination {
		stillOpen = append(stillOpen, datagram)
	}
	d.datagramByDestination = make(map[spec.Destination]spec.Datagram)
	close(d.ended)
	d.mutex.Unlock()

	for _, datagram := range stillOpen {
		datagram.Close()
	}
	return nil
}

// There is no one socket: each destination has its own stream to the server.
func (d *datagramOverStreamPerDestination) LocalAddr() net.Addr { return &net.UDPAddr{} }
