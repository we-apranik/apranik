package trojan

import (
	"bytes"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
	"github.com/we-apranik/apranik/spec"
)

type repeatingStream struct{ reader io.Reader }

func (s repeatingStream) Read(buffer []byte) (int, error) { return s.reader.Read(buffer) }
func (repeatingStream) Write(buffer []byte) (int, error)  { return len(buffer), nil }
func (repeatingStream) Close() error                      { return nil }
func (repeatingStream) CloseWrite() error                 { return nil }
func (repeatingStream) LocalAddr() net.Addr               { return nil }
func (repeatingStream) RemoteAddr() net.Addr              { return nil }
func (repeatingStream) SetDeadline(time.Time) error       { return nil }
func (repeatingStream) SetReadDeadline(time.Time) error   { return nil }
func (repeatingStream) SetWriteDeadline(time.Time) error  { return nil }

type endlessRepeat struct {
	frame  []byte
	offset int
}

func (r *endlessRepeat) Read(buffer []byte) (int, error) {
	copied := copy(buffer, r.frame[r.offset:])
	r.offset = (r.offset + copied) % len(r.frame)
	return copied, nil
}

func benchmarkIPDestination(b *testing.B) spec.Destination {
	b.Helper()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr("1.1.1.1"), 53)
	if err != nil {
		b.Fatal(err)
	}
	return destination
}

func BenchmarkStreamDatagramWriteTo(b *testing.B) {
	datagram := newStreamDatagram(repeatingStream{reader: bytes.NewReader(nil)})
	destination := benchmarkIPDestination(b)
	payload := make([]byte, 512)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := datagram.WriteTo(payload, destination); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStreamDatagramReadFrom(b *testing.B) {
	frame, err := wire.AppendUDPFrame(nil, benchmarkIPDestination(b), make([]byte, 512))
	if err != nil {
		b.Fatal(err)
	}
	datagram := newStreamDatagram(repeatingStream{reader: &endlessRepeat{frame: frame}})
	payload := make([]byte, wire.MaxUDPPayload)

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := datagram.ReadFrom(payload); err != nil {
			b.Fatal(err)
		}
	}
}
