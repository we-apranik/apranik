package trojan

import (
	"bufio"
	"net"
	"sync"

	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/spec"
)

// streamDatagram carries datagrams as frames inside a stream, both directions,
// once the request header is behind it. Nothing else reads the stream after
// that, so buffering its reads is safe.
//
// Safe for one reader and many writers, which is what a relay needs.
type streamDatagram struct {
	stream spec.Stream
	reader *bufio.Reader

	writeMutex sync.Mutex

	frameBuffer []byte

	unsentRequestPasswordHash *wire.PasswordHash
}

func newStreamDatagram(stream spec.Stream) *streamDatagram {
	return &streamDatagram{stream: stream, reader: bufio.NewReader(stream)}
}

func newClientStreamDatagram(stream spec.Stream, passwordHash wire.PasswordHash) *streamDatagram {
	datagram := newStreamDatagram(stream)
	datagram.unsentRequestPasswordHash = &passwordHash
	return datagram
}

func (d *streamDatagram) ReadFrom(payload []byte) (int, spec.Destination, error) {
	return wire.ReadUDPFrame(d.reader, payload)
}

func (d *streamDatagram) WriteTo(payload []byte, destination spec.Destination) (int, error) {
	d.writeMutex.Lock()
	defer d.writeMutex.Unlock()

	message := d.frameBuffer[:0]
	if d.unsentRequestPasswordHash != nil {
		request, err := wire.AppendRequest(message, wire.Request{
			PasswordHash: *d.unsentRequestPasswordHash,
			Command:      wire.CommandUDPAssociate,
			Destination:  destination,
		})
		if err != nil {
			return 0, err
		}
		message = request
	}
	message, err := wire.AppendUDPFrame(message, destination, payload)
	if err != nil {
		return 0, err
	}
	d.frameBuffer = message
	if _, err := d.stream.Write(message); err != nil {
		return 0, err
	}
	d.unsentRequestPasswordHash = nil
	return len(payload), nil
}

func (d *streamDatagram) Close() error { return d.stream.Close() }

func (d *streamDatagram) LocalAddr() net.Addr { return d.stream.LocalAddr() }
