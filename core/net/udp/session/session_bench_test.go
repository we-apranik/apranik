package session_test

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/The-Apranik/Apranik/core/net/udp/session"
	"github.com/The-Apranik/Apranik/spec"
)

// benchDatagram replies to nothing it is sent; it hands out the same reply
// forever until closed, so a benchmark measures only the session.
type benchDatagram struct {
	reply     []byte
	from      spec.Destination
	closed    chan struct{}
	closeOnce sync.Once
}

func (d *benchDatagram) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	select {
	case <-d.closed:
		return 0, spec.Destination{}, net.ErrClosed
	default:
		return copy(buffer, d.reply), d.from, nil
	}
}
func (*benchDatagram) WriteTo(payload []byte, _ spec.Destination) (int, error) {
	return len(payload), nil
}
func (d *benchDatagram) Close() error      { d.closeOnce.Do(func() { close(d.closed) }); return nil }
func (*benchDatagram) LocalAddr() net.Addr { return nil }

type benchOutbound struct{ datagram *benchDatagram }

func (benchOutbound) Tag() string { return "bench" }
func (benchOutbound) Networks() []spec.Network {
	return []spec.Network{spec.NetworkTCP, spec.NetworkUDP}
}
func (benchOutbound) Close() error { return nil }
func (benchOutbound) DialStream(context.Context, spec.Metadata) (spec.Stream, error) {
	return nil, spec.ErrUnsupportedNetwork
}
func (o benchOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return o.datagram, nil
}

func startBenchSession(b *testing.B) (*session.Session, spec.Outbound, spec.Destination) {
	b.Helper()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr("1.1.1.1"), 53)
	if err != nil {
		b.Fatal(err)
	}
	outbound := benchOutbound{datagram: &benchDatagram{reply: make([]byte, 512), from: destination, closed: make(chan struct{})}}
	udpSession := session.New(context.Background(), session.Config{})
	b.Cleanup(func() { udpSession.Close() })
	return udpSession, outbound, destination
}

func BenchmarkSessionSend(b *testing.B) {
	udpSession, outbound, destination := startBenchSession(b)
	payload := make([]byte, 512)

	b.ReportAllocs()
	for b.Loop() {
		if err := udpSession.Send(payload, destination, outbound); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSessionReply(b *testing.B) {
	udpSession, outbound, destination := startBenchSession(b)
	if err := udpSession.Send([]byte("open"), destination, outbound); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		reply := <-udpSession.Replies()
		reply.Release()
	}
}
