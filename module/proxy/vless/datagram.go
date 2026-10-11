package vless

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/The-Apranik/Apranik/module/proxy/vless/wire"
	"github.com/The-Apranik/Apranik/spec"
)

// ErrStreamServesAnotherDestination is a client datagram asked to send to a
// second destination. Plain VLESS names one destination per stream, in its
// request; core opens a stream per destination for it (the client says so with
// DatagramDestinationIsWritten), so this is a bug in the caller, not a
// condition to recover from.
var ErrStreamServesAnotherDestination = errors.New("vless: this stream serves another destination")

// clientDatagram carries the datagrams of ONE destination as length-prefixed
// frames inside a stream. The first WriteTo sends the request in front of its
// frame, naming that datagram's destination as the one this stream serves.
// Nothing else reads the stream after the answer header, so buffering its reads
// is safe.
//
// Safe for one reader and many writers, which is what a relay needs.
type clientDatagram struct {
	stream spec.Stream
	reader *bufio.Reader
	uuid   wire.UUID

	writeMutex  sync.Mutex
	frameBuffer []byte // reused for every frame; guarded by writeMutex
	requestSent bool   // guarded by writeMutex

	// Set once, by the first WriteTo, and read by the reader to say where each
	// datagram came from.
	servedDestination atomic.Pointer[spec.Destination]

	// Touched by the reader only.
	responseHeaderRead bool
}

func newClientDatagram(stream spec.Stream, uuid wire.UUID) *clientDatagram {
	return &clientDatagram{stream: stream, reader: bufio.NewReader(stream), uuid: uuid}
}

// The first call reads the server's answer header, which the server sends in
// front of its first frame. Every datagram is reported as coming from the
// destination the request named.
func (d *clientDatagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	if !d.responseHeaderRead {
		if err := wire.ReadResponseHeader(d.reader); err != nil {
			return 0, spec.Destination{}, err
		}
		d.responseHeaderRead = true
	}
	size, err := wire.ReadUDPFrame(d.reader, payload)
	if err != nil {
		return 0, spec.Destination{}, err
	}
	served := d.servedDestination.Load()
	if served == nil {
		return 0, spec.Destination{}, errors.New("vless: a datagram arrived before any was sent")
	}
	return size, *served, nil
}

// One Write per frame, under the lock, so frames from concurrent writers never
// interleave.
func (d *clientDatagram) WriteTo(payload []byte, destination spec.Destination) (int, error) {
	d.writeMutex.Lock()
	defer d.writeMutex.Unlock()

	message := d.frameBuffer[:0]
	if !d.requestSent {
		request, err := wire.AppendRequest(message, wire.Request{UUID: d.uuid, Command: wire.CommandUDP, Destination: destination})
		if err != nil {
			return 0, err
		}
		message = request
	} else if served := d.servedDestination.Load(); *served != destination {
		return 0, fmt.Errorf("%w: it serves %s, not %s", ErrStreamServesAnotherDestination, *served, destination)
	}

	message, err := wire.AppendUDPFrame(message, payload)
	if err != nil {
		return 0, err
	}
	d.frameBuffer = message
	if _, err := d.stream.Write(message); err != nil {
		return 0, err
	}

	if !d.requestSent {
		d.requestSent = true
		d.servedDestination.Store(&destination)
	}
	return len(payload), nil
}

func (d *clientDatagram) Close() error { return d.stream.Close() }

func (d *clientDatagram) LocalAddr() net.Addr { return d.stream.LocalAddr() }

// serverDatagram is the other end of a clientDatagram: it reports every frame
// as bound for the destination the request named, and answers with frames of
// its own, the answer header in front of the first. The destination argument of
// WriteTo is ignored: a reply belongs to the one destination of the stream, and
// a domain destination answers from an IP address, so the two cannot be compared.
//
// Safe for one reader and many writers, which is what a relay needs.
type serverDatagram struct {
	stream      spec.Stream
	reader      *bufio.Reader
	destination spec.Destination

	writeMutex         sync.Mutex
	frameBuffer        []byte // reused for every frame; guarded by writeMutex
	responseHeaderSent bool   // guarded by writeMutex
}

func newServerDatagram(stream spec.Stream, destination spec.Destination) *serverDatagram {
	return &serverDatagram{stream: stream, reader: bufio.NewReader(stream), destination: destination}
}

func (d *serverDatagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	size, err := wire.ReadUDPFrame(d.reader, payload)
	if err != nil {
		return 0, spec.Destination{}, err
	}
	return size, d.destination, nil
}

func (d *serverDatagram) WriteTo(payload []byte, _ spec.Destination) (int, error) {
	d.writeMutex.Lock()
	defer d.writeMutex.Unlock()

	message := d.frameBuffer[:0]
	if !d.responseHeaderSent {
		message = wire.AppendResponseHeader(message)
	}
	message, err := wire.AppendUDPFrame(message, payload)
	if err != nil {
		return 0, err
	}
	d.frameBuffer = message
	if _, err := d.stream.Write(message); err != nil {
		return 0, err
	}
	d.responseHeaderSent = true
	return len(payload), nil
}

func (d *serverDatagram) Close() error { return d.stream.Close() }

func (d *serverDatagram) LocalAddr() net.Addr { return d.stream.LocalAddr() }
