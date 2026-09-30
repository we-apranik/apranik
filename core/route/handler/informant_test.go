package handler

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/net/peekable"
	"github.com/The-Apranik/Apranik/spec"
)

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

type prefixSniffer struct{ calls *int }

func (s prefixSniffer) Sniff(prefix []byte) (spec.SniffResult, error) {
	*s.calls++
	return spec.SniffResult{Protocol: "test", Domain: string(prefix[:4])}, nil
}

func TestStreamInformantSniffsOnceAndReplays(t *testing.T) {
	clientSide, handlerSide := net.Pipe()
	defer clientSide.Close()
	go clientSide.Write([]byte("abcdefgh"))

	informant := newStreamTrafficInformant(spec.Metadata{}, pipeStream{handlerSide})
	calls := 0
	sniffers := []spec.Sniffer{prefixSniffer{&calls}}
	first := informant.Sniff(context.Background(), sniffers, time.Second)
	second := informant.Sniff(context.Background(), sniffers, time.Second)
	if first == nil || first.Domain != "abcd" || second != first || calls != 1 {
		t.Fatalf("got %v then %v after %d sniffer calls, want one sniff remembered", first, second, calls)
	}
	if informant.Metadata().Sniffed != first {
		t.Error("Metadata does not carry the sniff result")
	}

	relayed := make([]byte, 8)
	if _, err := io.ReadFull(informant.streamToRelay(), relayed); err != nil || string(relayed) != "abcdefgh" {
		t.Fatalf("relayed %q, %v: the peeked bytes were not replayed", relayed, err)
	}
}

func TestStreamInformantWithoutSniffRelaysTheStreamItself(t *testing.T) {
	stream := pipeStream{}
	informant := newStreamTrafficInformant(spec.Metadata{}, stream)
	if informant.streamToRelay() != spec.Stream(stream) {
		t.Fatal("a stream nobody sniffed was wrapped")
	}
}

func TestStreamInformantReusesAPeekableStream(t *testing.T) {
	clientSide, handlerSide := net.Pipe()
	defer clientSide.Close()
	go clientSide.Write([]byte("abcd"))
	alreadyPeekable := peekable.NewStream(pipeStream{handlerSide})

	informant := newStreamTrafficInformant(spec.Metadata{}, alreadyPeekable)
	calls := 0
	informant.Sniff(context.Background(), []spec.Sniffer{prefixSniffer{&calls}}, time.Second)
	if informant.streamToRelay() != spec.Stream(alreadyPeekable) {
		t.Fatal("a peekable stream was wrapped a second time")
	}
}

// Per TCP connection: the informant and a router with no rules. With rules,
// see the router's own benchmark.
func BenchmarkRouteStreamWithoutRules(b *testing.B) {
	router := defaultOnlyRouter{}
	metadata := spec.Metadata{Network: spec.NetworkTCP}
	stream := pipeStream{}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		informant := newStreamTrafficInformant(metadata, stream)
		router.Route(ctx, informant)
		_ = informant.streamToRelay()
	}
}

type defaultOnlyRouter struct{}

func (defaultOnlyRouter) Route(_ context.Context, informant component.RouterTrafficInformant) (component.Decision, error) {
	_ = informant.Metadata()
	return component.Decision{Rule: "default"}, nil
}
