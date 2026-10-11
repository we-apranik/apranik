package muxcool

import (
	"fmt"
	"io"
	"net"

	"github.com/The-Apranik/Apranik/spec"
)

// datagram is a UDP sub-connection. One frame is one datagram. It is safe for
// one reader and many writers.
type datagram struct {
	*subConnection
}

var _ spec.Datagram = (*datagram)(nil)

// ReadFrom reads the next datagram and says where it is from: the address its
// Keep frame names, or else the destination this sub-connection is bound to. A
// datagram larger than p is dropped and reported as ErrDatagramTooLarge; the
// next ReadFrom reads the one after it. A datagram of no bytes is skipped, as
// Xray and sing-box skip it.
func (d *datagram) ReadFrom(p []byte) (int, spec.Destination, error) {
	remaining, source, err := d.awaitDelivery(d.readDeadline.wait())
	if err != nil {
		return 0, spec.Destination{}, err
	}

	if remaining > len(p) {
		discarded, discardErr := d.conn.reader.Discard(remaining)
		d.finishRead(discarded, discardErr)
		return 0, spec.Destination{}, fmt.Errorf("%w: %d bytes, the buffer holds %d", ErrDatagramTooLarge, remaining, len(p))
	}

	read, readErr := io.ReadFull(d.conn.reader, p[:remaining])
	d.finishRead(read, readErr)
	if readErr != nil {
		return 0, spec.Destination{}, closedError(readErr)
	}
	if !source.IsValid() {
		source = d.destination
	}
	return remaining, source, nil
}

// WriteTo sends one datagram. At the client end the destination must be the one
// this sub-connection was opened for; at the server end it is the source the
// reply comes from, which plain Mux.Cool does not carry, so it is not used.
func (d *datagram) WriteTo(p []byte, to spec.Destination) (int, error) {
	if !d.accepted && to != d.destination {
		return 0, fmt.Errorf("%w: %s, not %s", ErrDestinationNotBound, to, d.destination)
	}
	if len(p) > MaxDataLength {
		return 0, fmt.Errorf("%w: %d bytes, a frame carries at most %d", ErrDatagramTooLarge, len(p), MaxDataLength)
	}
	if err := d.writable(); err != nil {
		return 0, err
	}
	if err := d.writeDatagram(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (d *datagram) Close() error { return d.close() }

func (d *datagram) LocalAddr() net.Addr { return subConnectionAddress(d.id) }
