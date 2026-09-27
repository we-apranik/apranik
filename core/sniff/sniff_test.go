package sniff_test

import (
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/net/peekable"
	"github.com/The-Apranik/Apranik/core/sniff"
	httpsniffer "github.com/The-Apranik/Apranik/module/sniffer/http"
	tlssniffer "github.com/The-Apranik/Apranik/module/sniffer/tls"
	"github.com/The-Apranik/Apranik/spec"
)

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

func newPipe(t *testing.T) (clientSide net.Conn, inboundSide *peekable.Stream) {
	t.Helper()
	client, inbound := net.Pipe()
	t.Cleanup(func() { client.Close(); inbound.Close() })
	return client, peekable.NewStream(pipeStream{inbound})
}

var bothSniffers = []spec.Sniffer{tlssniffer.NewSnifferModule(), httpsniffer.NewSnifferModule()}

func TestARecognisedRequestIsReportedAndReplayed(t *testing.T) {
	client, inboundSide := newPipe(t)
	request := "GET / HTTP/1.1\r\nHost: example.com\r\n\r\nbody"
	go client.Write([]byte(request))

	result := sniff.Recognise(inboundSide, bothSniffers, time.Second)
	if result == nil || result.Protocol != "http" || result.Domain != "example.com" {
		t.Fatalf("result = %+v, want http example.com", result)
	}

	replayed := make([]byte, len(request))
	if _, err := io.ReadFull(inboundSide, replayed); err != nil {
		t.Fatalf("reading the replayed stream: %v", err)
	}
	if string(replayed) != request {
		t.Errorf("replayed %q, want %q", replayed, request)
	}
}

func TestASilentClientIsGivenUpOnAfterTheTimeout(t *testing.T) {
	client, inboundSide := newPipe(t)

	started := time.Now()
	result := sniff.Recognise(inboundSide, bothSniffers, 100*time.Millisecond)
	if waited := time.Since(started); waited < 100*time.Millisecond || waited > time.Second {
		t.Errorf("waited %s, want about the 100ms timeout", waited)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}

	go client.Write([]byte("late"))
	late := make([]byte, 4)
	inboundSide.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(inboundSide, late); err != nil || string(late) != "late" {
		t.Errorf("after the timeout the stream read %q, %v; want it usable", late, err)
	}
}

func TestAnUnknownProtocolStopsWithoutWaitingForTheTimeout(t *testing.T) {
	client, inboundSide := newPipe(t)
	go client.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n"))

	started := time.Now()
	result := sniff.Recognise(inboundSide, bothSniffers, 5*time.Second)
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("waited %s for a stream every sniffer had already refused", waited)
	}
}

func TestNoSniffersReadsNothing(t *testing.T) {
	_, inboundSide := newPipe(t)

	result := sniff.Recognise(inboundSide, nil, time.Second)
	if result != nil || inboundSide.Buffered() != 0 {
		t.Errorf("got %+v with %d bytes buffered; want no result and nothing read", result, inboundSide.Buffered())
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
	result := sniff.Recognise(inboundSide, []spec.Sniffer{httpsniffer.NewSnifferModule()}, 5*time.Second)
	if result == nil || result.Domain != "example.com" {
		t.Fatalf("result = %+v", result)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("sniffing waited %v for bytes the client never sends", waited)
	}
}

// Bytes the stream already holds, such as a handshake's leftovers, are shown
// to the sniffers before anything is read or waited for.
func TestBytesAlreadyBufferedAreSniffedWithoutWaiting(t *testing.T) {
	clientSide, inboundSide := newPipe(t)
	go clientSide.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	if _, err := inboundSide.Peek(len("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	result := sniff.Recognise(inboundSide, bothSniffers, 5*time.Second)
	if result == nil || result.Domain != "example.com" {
		t.Fatalf("result = %+v", result)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("waited %v with the whole request already buffered", waited)
	}
}

// A client that hangs up mid-message ends sniffing at once, not at the timeout.
func TestAClientThatClosesMidMessageEndsSniffingAtOnce(t *testing.T) {
	clientSide, inboundSide := newPipe(t)
	go func() {
		clientSide.Write([]byte("GET / HTTP/1.1\r\nHost: exa"))
		clientSide.Close()
	}()

	started := time.Now()
	if result := sniff.Recognise(inboundSide, bothSniffers, 5*time.Second); result != nil {
		t.Errorf("result = %+v, want nil for a cut-off request", result)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("waited %v after the client had gone", waited)
	}
}

// A real ClientHello, captured once from crypto/tls.
func clientHello(b *testing.B) []byte {
	b.Helper()
	clientSide, serverSide := net.Pipe()
	go tls.Client(clientSide, &tls.Config{ServerName: "example.com"}).Handshake()
	header := make([]byte, 5)
	if _, err := io.ReadFull(serverSide, header); err != nil {
		b.Fatal(err)
	}
	body := make([]byte, int(header[3])<<8|int(header[4]))
	if _, err := io.ReadFull(serverSide, body); err != nil {
		b.Fatal(err)
	}
	clientSide.Close()
	serverSide.Close()
	return append(header, body...)
}

func BenchmarkSniffTLSClientHello(b *testing.B) {
	hello := clientHello(b)
	sniffers := []spec.Sniffer{tlssniffer.NewSnifferModule()}
	b.ReportAllocs()
	for b.Loop() {
		clientSide, inboundSide := net.Pipe()
		go clientSide.Write(hello)
		result := sniff.Recognise(peekable.NewStream(pipeStream{inboundSide}), sniffers, 0)
		if result == nil || result.Domain != "example.com" {
			b.Fatalf("result = %+v", result)
		}
		clientSide.Close()
		inboundSide.Close()
	}
}
