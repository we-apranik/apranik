package sniff_test

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/sniff"
	httpsniffer "github.com/The-Apranik/Apranik/module/sniffer/http"
	tlssniffer "github.com/The-Apranik/Apranik/module/sniffer/tls"
	"github.com/The-Apranik/Apranik/spec"
)

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

func newPipe(t *testing.T) (clientSide net.Conn, inboundSide spec.Stream) {
	t.Helper()
	client, inbound := net.Pipe()
	t.Cleanup(func() { client.Close(); inbound.Close() })
	return client, pipeStream{inbound}
}

var bothSniffers = []spec.Sniffer{tlssniffer.NewSnifferModule(), httpsniffer.NewSnifferModule()}

func TestARecognisedRequestIsReportedAndReplayed(t *testing.T) {
	client, inboundSide := newPipe(t)
	request := "GET / HTTP/1.1\r\nHost: example.com\r\n\r\nbody"
	go client.Write([]byte(request))

	sniffed, result := sniff.Stream(inboundSide, bothSniffers, time.Second)
	if result == nil || result.Protocol != "http" || result.Domain != "example.com" {
		t.Fatalf("result = %+v, want http example.com", result)
	}

	replayed := make([]byte, len(request))
	if _, err := io.ReadFull(sniffed, replayed); err != nil {
		t.Fatalf("reading the replayed stream: %v", err)
	}
	if string(replayed) != request {
		t.Errorf("replayed %q, want %q", replayed, request)
	}
}

func TestASilentClientIsGivenUpOnAfterTheTimeout(t *testing.T) {
	client, inboundSide := newPipe(t)

	started := time.Now()
	sniffed, result := sniff.Stream(inboundSide, bothSniffers, 100*time.Millisecond)
	if waited := time.Since(started); waited < 100*time.Millisecond || waited > time.Second {
		t.Errorf("waited %s, want about the 100ms timeout", waited)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}

	go client.Write([]byte("late"))
	late := make([]byte, 4)
	sniffed.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(sniffed, late); err != nil || string(late) != "late" {
		t.Errorf("after the timeout the stream read %q, %v; want it usable", late, err)
	}
}

func TestAnUnknownProtocolStopsWithoutWaitingForTheTimeout(t *testing.T) {
	client, inboundSide := newPipe(t)
	go client.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n"))

	started := time.Now()
	_, result := sniff.Stream(inboundSide, bothSniffers, 5*time.Second)
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("waited %s for a stream every sniffer had already refused", waited)
	}
}

func TestNoSniffersLeavesTheStreamUntouched(t *testing.T) {
	_, inboundSide := newPipe(t)

	sniffed, result := sniff.Stream(inboundSide, nil, time.Second)
	if sniffed != inboundSide || result != nil {
		t.Errorf("got %v, %+v; want the same stream and no result", sniffed, result)
	}
}

// A client that sends its first message in pieces and then waits for the
// server, as every TLS and HTTP client does, must be recognised as soon as the
// last piece arrives, not when the timeout gives up waiting for a byte that
// will never come.
func TestAMessageInPiecesIsRecognisedWithoutWaitingForTheTimeout(t *testing.T) {
	clientSide, inboundSide := newPipe(t)
	request := "GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"
	go func() {
		for _, piece := range []string{request[:5], request[5:20], request[20:]} {
			clientSide.Write([]byte(piece))
		}
	}()

	started := time.Now()
	_, result := sniff.Stream(inboundSide, []spec.Sniffer{httpsniffer.NewSnifferModule()}, 5*time.Second)
	if result == nil || result.Domain != "example.com" {
		t.Fatalf("result = %+v", result)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("sniffing waited %v for bytes the client never sends", waited)
	}
}
