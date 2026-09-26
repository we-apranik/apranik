package nat_test

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/spec"
)

// benchSocket stands in for freedom's UDP socket: writes vanish, and reads
// return the same reply from the same sender until closed.
type benchSocket struct {
	reply     []byte
	from      spec.Destination
	closed    chan struct{}
	closeOnce sync.Once
}

func (s *benchSocket) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	select {
	case <-s.closed:
		return 0, spec.Destination{}, net.ErrClosed
	default:
		return copy(buffer, s.reply), s.from, nil
	}
}
func (*benchSocket) WriteTo(payload []byte, _ spec.Destination) (int, error) {
	return len(payload), nil
}
func (s *benchSocket) Close() error      { s.closeOnce.Do(func() { close(s.closed) }); return nil }
func (*benchSocket) LocalAddr() net.Addr { return nil }

func startBenchNAT(b *testing.B) (*nat.Datagram, spec.Destination) {
	b.Helper()
	peer, err := spec.NewDestinationFromIP(netip.MustParseAddr("1.1.1.1"), 53)
	if err != nil {
		b.Fatal(err)
	}
	create := func(context.Context) (spec.Datagram, error) {
		return &benchSocket{reply: make([]byte, 512), from: peer, closed: make(chan struct{})}, nil
	}
	natDatagram, err := nat.New(context.Background(), create, nat.NewDefaultPolicy(), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { natDatagram.Close() })
	return natDatagram, peer
}

func BenchmarkNATWriteTo(b *testing.B) {
	natDatagram, peer := startBenchNAT(b)
	payload := make([]byte, 512)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := natDatagram.WriteTo(payload, peer); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNATReadFrom(b *testing.B) {
	natDatagram, peer := startBenchNAT(b)
	if _, err := natDatagram.WriteTo([]byte("open"), peer); err != nil {
		b.Fatal(err)
	}
	buffer := make([]byte, 65507)

	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := natDatagram.ReadFrom(buffer); err != nil {
			b.Fatal(err)
		}
	}
}
