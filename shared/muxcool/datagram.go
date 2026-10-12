package muxcool

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/The-Apranik/Apranik/spec"
)

// datagram is safe for one reader and many writers.
type datagram struct {
	*subConnection
}

var _ spec.Datagram = (*datagram)(nil)

// ReadFrom drops a datagram larger than p as ErrDatagramTooLarge and skips an empty one.
func (d *datagram) ReadFrom(p []byte) (int, spec.Destination, error) {
	remaining, source, err := d.claimDelivery(d.readDeadline.wait())
	if err != nil {
		return 0, spec.Destination{}, err
	}

	if remaining > len(p) {
		discarded, discardErr := d.conn.reader.Discard(remaining)
		d.finishDelivery(discarded, discardErr)
		return 0, spec.Destination{}, fmt.Errorf("%w: %d bytes, the buffer holds %d", ErrDatagramTooLarge, remaining, len(p))
	}

	read, readErr := io.ReadFull(d.conn.reader, p[:remaining])
	d.finishDelivery(read, readErr)
	if readErr != nil {
		return 0, spec.Destination{}, closedError(readErr)
	}
	if !source.IsValid() {
		source = d.destination
	}
	return remaining, source, nil
}

// WriteTo: to is the destination (client end; the bound one unless XUDP) or the reply's source (server end).
func (d *datagram) WriteTo(p []byte, to spec.Destination) (int, error) {
	if d.addressInEveryDatagram.Load() {
		if !to.IsValid() {
			return 0, errors.New("muxcool: WriteTo needs a destination because every datagram names its address")
		}
	} else if !d.accepted && to != d.destination {
		return 0, fmt.Errorf("%w: %s, not %s", ErrDestinationNotBound, to, d.destination)
	}
	if len(p) > MaxDataLength {
		return 0, fmt.Errorf("%w: %d bytes, a frame carries at most %d", ErrDatagramTooLarge, len(p), MaxDataLength)
	}
	if err := d.writable(); err != nil {
		return 0, err
	}
	if err := d.writeDatagram(p, to); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (d *datagram) Close() error { return d.close() }

func (d *datagram) LocalAddr() net.Addr { return subConnectionAddress(d.id) }
