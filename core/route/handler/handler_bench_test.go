package handler_test

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/we-apranik/apranik/core/route/handler"
	"github.com/we-apranik/apranik/core/route/router"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

// One short connection through the handler, from accept to the handler
// returning: what a statistics setting adds to every connection.
func benchmarkHandleOneShortStream(b *testing.B, registry *stats.Registry) {
	out := capturingOutbound{tag: "out", dialed: make(chan net.Conn, 1)}
	trafficHandler := handler.New(router.NewWithDefaultOutbound(out), handler.Config{Stats: registry})
	metadata := spec.Metadata{Network: spec.NetworkTCP, InboundTag: "in", User: "alice"}
	request, response := make([]byte, 512), make([]byte, 4096)
	b.ReportAllocs()
	for b.Loop() {
		client, inboundSide := net.Pipe()
		returned := make(chan error, 1)
		go func() {
			returned <- trafficHandler.HandleStream(context.Background(), blockTestStream{inboundSide}, metadata)
		}()
		remote := <-out.dialed

		go client.Write(request)
		io.ReadFull(remote, make([]byte, len(request)))
		go remote.Write(response)
		io.ReadFull(client, make([]byte, len(response)))

		client.Close()
		remote.Close()
		<-returned
	}
}

func BenchmarkHandleOneShortStreamWithoutStatistics(b *testing.B) {
	benchmarkHandleOneShortStream(b, nil)
}

func BenchmarkHandleOneShortStreamCounting(b *testing.B) {
	benchmarkHandleOneShortStream(b, stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"}, Outbounds: []stats.OutboundDescription{{Tag: "out"}},
		RuleLocations: []string{router.DefaultRuleLocation},
	}))
}

func BenchmarkHandleOneShortStreamCountingWithLiveConversations(b *testing.B) {
	benchmarkHandleOneShortStream(b, stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"}, Outbounds: []stats.OutboundDescription{{Tag: "out"}},
		RuleLocations: []string{router.DefaultRuleLocation}, TrackLiveConversations: true,
	}))
}

func BenchmarkHandleOneShortStreamCountingWithDialHealth(b *testing.B) {
	benchmarkHandleOneShortStream(b, stats.NewRegistry(stats.Config{
		InboundTags: []string{"in"}, Outbounds: []stats.OutboundDescription{{Tag: "out"}},
		RuleLocations: []string{router.DefaultRuleLocation}, TimeDials: true,
	}))
}
