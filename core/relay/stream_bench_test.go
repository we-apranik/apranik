package relay_test

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/The-Apranik/Apranik/core/relay"
)

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
