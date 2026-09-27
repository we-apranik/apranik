package relay_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/relay"
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
		ended <- relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, idleTimeout)
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
			ended <- relay.Stream(context.Background(), pipeStream{clientSide}, pipeStream{remoteSide}, relay.DefaultStreamIdleTimeout)
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
