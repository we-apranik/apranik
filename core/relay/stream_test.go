package relay_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/relay"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

func relayedPipes(t *testing.T, idleTimeout time.Duration) (client, remote net.Conn, relayEnded <-chan error) {
	t.Helper()

	client, clientSide := net.Pipe()
	remoteSide, remote := net.Pipe()
	ended := make(chan error, 1)
	go func() {
		ended <- relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, idleTimeout, nil, nil)
	}()
	t.Cleanup(func() { client.Close(); remote.Close() })
	return client, remote, ended
}

func TestASilentStreamEndsAfterTheIdleTimeout(t *testing.T) {
	_, _, relayEnded := relayedPipes(t, 100*time.Millisecond)

	select {
	case err := <-relayEnded:
		if !errors.Is(err, relay.ErrStreamIdle) {
			t.Errorf("relay ended with %v, want ErrStreamIdle", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a silent stream was not ended")
	}
}

func TestTrafficInOneDirectionKeepsTheStreamAlive(t *testing.T) {
	client, remote, relayEnded := relayedPipes(t, 150*time.Millisecond)

	go func() {
		buffer := make([]byte, 16)
		for {
			if _, err := remote.Read(buffer); err != nil {
				return
			}
		}
	}()

	for range 8 {
		if _, err := client.Write([]byte("tick")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	select {
	case err := <-relayEnded:
		t.Fatalf("an active stream was ended: %v", err)
	default:
	}
}

func TestNoIdleTimeoutMeansNoIdleBound(t *testing.T) {
	_, _, relayEnded := relayedPipes(t, 0)

	select {
	case err := <-relayEnded:
		t.Fatalf("a stream with no idle bound ended: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
}

// One short request and response per connection, the shape that makes a
// per-connection copy buffer matter.
func BenchmarkStreamRelayOneShortExchange(b *testing.B) {
	request, response := make([]byte, 512), make([]byte, 4096)
	b.ReportAllocs()
	for b.Loop() {
		client, clientSide := net.Pipe()
		remoteSide, remote := net.Pipe()
		ended := make(chan error, 1)
		go func() {
			ended <- relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, relay.DefaultStreamIdleTimeout, nil, nil)
		}()

		go client.Write(request)
		io.ReadFull(remote, make([]byte, len(request)))
		go remote.Write(response)
		io.ReadFull(client, make([]byte, len(response)))

		client.Close()
		remote.Close()
		<-ended
	}
}

func TestRelayedBytesAreCountedAsUplinkAndDownlinkOnEveryCounters(t *testing.T) {
	var first, second stats.Counters
	client, clientSide := net.Pipe()
	remoteSide, remote := net.Pipe()
	ended := make(chan error, 1)
	go func() {
		ended <- relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, 0,
			[]*stats.Counters{&first, nil, &second}, nil)
	}()

	request, response := make([]byte, 700), make([]byte, 5000)
	go client.Write(request)
	io.ReadFull(remote, make([]byte, len(request)))
	go remote.Write(response)
	io.ReadFull(client, make([]byte, len(response)))
	client.Close()
	remote.Close()
	<-ended

	for name, counters := range map[string]*stats.Counters{"first": &first, "second": &second} {
		if got := counters.Snapshot().UplinkBytes; got != uint64(len(request)) {
			t.Errorf("%s uplink = %d, want %d", name, got, len(request))
		}
		if got := counters.Snapshot().DownlinkBytes; got != uint64(len(response)) {
			t.Errorf("%s downlink = %d, want %d", name, got, len(response))
		}
	}
}

func TestBytesAreCountedWhileTheConnectionIsStillOpen(t *testing.T) {
	var counters stats.Counters
	client, clientSide := net.Pipe()
	remoteSide, remote := net.Pipe()
	go relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, 0, []*stats.Counters{&counters}, nil)
	t.Cleanup(func() { client.Close(); remote.Close() })

	go client.Write(make([]byte, 100))
	io.ReadFull(remote, make([]byte, 100))

	if got := counters.Snapshot().UplinkBytes; got != 100 {
		t.Errorf("uplink = %d while the connection is open, want 100", got)
	}
}

func TestRelayedBytesAreCountedOnTheConnectionsOwnBytesToo(t *testing.T) {
	registry := stats.NewRegistry(stats.Config{TrackLiveConversations: true})
	connection := registry.TrackConversation(stats.ConversationInfo{}, nil)
	client, clientSide := net.Pipe()
	remoteSide, remote := net.Pipe()
	go relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, 0, nil, connection.Bytes())
	t.Cleanup(func() { client.Close(); remote.Close() })

	go client.Write(make([]byte, 70))
	io.ReadFull(remote, make([]byte, 70))
	go remote.Write(make([]byte, 500))
	io.ReadFull(client, make([]byte, 500))

	live := registry.LiveConversations()
	if len(live) != 1 || live[0].UplinkBytes != 70 || live[0].DownlinkBytes != 500 {
		t.Errorf("live conversations = %+v, want one with 70 up and 500 down", live)
	}
}

func BenchmarkStreamRelayOneShortExchangeCounted(b *testing.B) {
	var inbound, outbound, user stats.Counters
	counters := []*stats.Counters{&inbound, &outbound, &user}
	request, response := make([]byte, 512), make([]byte, 4096)
	b.ReportAllocs()
	for b.Loop() {
		client, clientSide := net.Pipe()
		remoteSide, remote := net.Pipe()
		ended := make(chan error, 1)
		go func() {
			ended <- relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, relay.DefaultStreamIdleTimeout, counters, nil)
		}()

		go client.Write(request)
		io.ReadFull(remote, make([]byte, len(request)))
		go remote.Write(response)
		io.ReadFull(client, make([]byte, len(response)))

		client.Close()
		remote.Close()
		<-ended
	}
}

// Bulk transfer over a real loopback connection, where reads are large and
// the cost of counting each one is what matters.
func benchmarkStreamRelayBulk(b *testing.B, counters []*stats.Counters) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()
	sink := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		sink <- conn
	}()
	remoteEnd, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	remoteSide := <-sink

	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer clientListener.Close()
	go func() {
		conn, _ := clientListener.Accept()
		sink <- conn
	}()
	client, err := net.Dial("tcp", clientListener.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	clientSide := <-sink

	go relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteEnd}, relay.DefaultStreamIdleTimeout, counters, nil)
	go io.Copy(io.Discard, remoteSide)
	defer client.Close()
	defer remoteSide.Close()

	chunk := make([]byte, 32<<10)
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := client.Write(chunk); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStreamRelayBulkUncounted(b *testing.B) { benchmarkStreamRelayBulk(b, nil) }

func BenchmarkStreamRelayBulkCountedUnderThreeCounters(b *testing.B) {
	benchmarkStreamRelayBulk(b, []*stats.Counters{{}, {}, {}})
}
