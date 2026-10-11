// Package vless_test exercises the vless module through spec alone: no
// registry, no builder, no relay. The underlying stream is a net.Pipe.
package vless_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/proxy/vless"
	"github.com/The-Apranik/Apranik/module/proxy/vless/wire"
	"github.com/The-Apranik/Apranik/spec"
)

const (
	aliceUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"
	bobUUID   = "a3482e88-686a-4a58-8126-99c9df64b7bf"
)

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

// bufferedStream is the smallest spec.PeekableStream, standing in for core's.
type bufferedStream struct {
	spec.Stream
	reader *bufio.Reader
}

func (s bufferedStream) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s bufferedStream) Peek(n int) ([]byte, error) { return s.reader.Peek(n) }

// A bufio.Reader always reads ahead, so this stand-in cannot keep the promise;
// VLESS never asks for it.
func (bufferedStream) PeekExactly(int) ([]byte, error) { panic("vless does not use PeekExactly") }
func newBufferedStream(s spec.Stream) bufferedStream   { return bufferedStream{s, bufio.NewReader(s)} }

func pipe(t *testing.T) (clientSide, serverSide spec.Stream) {
	t.Helper()
	x, y := net.Pipe()
	t.Cleanup(func() { x.Close(); y.Close() })
	return pipeStream{x}, pipeStream{y}
}

var serverSettings = vless.Settings{Users: []vless.User{
	{Name: "alice", UUID: aliceUUID},
	{Name: "bob", UUID: bobUUID},
}}

type server interface {
	spec.StreamProxyServer
	spec.DatagramOverStreamProxyServer
}

type client interface {
	spec.StreamProxyClient
	spec.DatagramOverStreamProxyClient
}

func newServer(t *testing.T) server {
	t.Helper()
	streamServer, err := (&vless.Proxy{Settings: serverSettings}).NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	return streamServer.(server)
}

func newClient(t *testing.T, uuid types.Secret) client {
	t.Helper()
	return newClientWith(t, vless.Settings{UUID: uuid})
}

func newClientWith(t *testing.T, settings vless.Settings) client {
	t.Helper()
	streamClient, err := (&vless.Proxy{Settings: settings}).NewStreamClient()
	if err != nil {
		t.Fatal(err)
	}
	return streamClient.(client)
}

func domainDestination(domain string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		panic(err)
	}
	return destination
}

func ipDestination(ip string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
	if err != nil {
		panic(err)
	}
	return destination
}

type accepted struct {
	isDatagram bool
	stream     spec.Stream
	datagram   spec.Datagram
	metadata   spec.Metadata
	err        error
}

// serve does what core does: identify, then call the matching accept.
func serve(t *testing.T, underlyingStream spec.Stream) <-chan accepted {
	server := newServer(t)
	results := make(chan accepted, 1)
	go func() {
		peekable := newBufferedStream(underlyingStream)
		isDatagram, err := server.IsDatagramOverStream(context.Background(), peekable)
		if err != nil {
			results <- accepted{err: err}
			return
		}
		if isDatagram {
			datagram, metadata, err := server.AcceptDatagramOverStream(context.Background(), peekable)
			results <- accepted{isDatagram: true, datagram: datagram, metadata: metadata, err: err}
			return
		}
		stream, metadata, err := server.AcceptStream(context.Background(), peekable)
		results <- accepted{stream: stream, metadata: metadata, err: err}
	}()
	return results
}

func receive(t *testing.T, results <-chan accepted) accepted {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("the server never finished accepting")
		return accepted{}
	}
}

func TestConnectRoundTripKeepsTheEarlyPayloadAndPutsTheAnswerHeaderOnlyBeforeServerData(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)

	destination := domainDestination("example.com", 443)
	upstream, err := newClient(t, aliceUUID).RequestStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkTCP, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	go upstream.Write([]byte("GET / HTTP/1.1\r\n\r\n"))

	result := receive(t, results)
	if result.err != nil || result.isDatagram {
		t.Fatalf("accept = %+v, want a stream", result)
	}
	if result.metadata.User != "alice" || result.metadata.Destination != destination || result.metadata.Network != spec.NetworkTCP {
		t.Errorf("metadata = %+v, want alice to %s over tcp", result.metadata, destination)
	}

	early := make([]byte, len("GET / HTTP/1.1\r\n\r\n"))
	if _, err := io.ReadFull(result.stream, early); err != nil || string(early) != "GET / HTTP/1.1\r\n\r\n" {
		t.Fatalf("early payload = %q, %v", early, err)
	}

	go result.stream.Write([]byte("HTTP/1.1 200 OK"))
	answer := make([]byte, len("HTTP/1.1 200 OK"))
	if _, err := io.ReadFull(upstream, answer); err != nil || string(answer) != "HTTP/1.1 200 OK" {
		t.Errorf("answer = %q, %v, want the payload with the header already dropped", answer, err)
	}
}

// writeRecordingStream keeps each Write separately, so a test can tell one
// segment carrying header and payload from two.
type writeRecordingStream struct {
	pipeStream
	writes chan []byte
}

func (s writeRecordingStream) Write(p []byte) (int, error) {
	s.writes <- append([]byte(nil), p...)
	return len(p), nil
}

func (writeRecordingStream) Close() error { return nil }

func newWriteRecordingStream() writeRecordingStream {
	return writeRecordingStream{writes: make(chan []byte, 8)}
}

func TestConnectSendsTheRequestWithTheFirstPayload(t *testing.T) {
	underlyingStream := newWriteRecordingStream()
	destination := domainDestination("example.com", 443)
	upstream, err := newClient(t, aliceUUID).RequestStream(context.Background(), underlyingStream, spec.Metadata{Network: spec.NetworkTCP, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case written := <-underlyingStream.writes:
		t.Fatalf("wrote %q before any payload", written)
	case <-time.After(20 * time.Millisecond):
	}

	upstream.Write([]byte("hello"))
	first := <-underlyingStream.writes
	request, err := wire.ReadRequest(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("first write is not a request: %v", err)
	}
	if request.Destination != destination || request.Command != wire.CommandTCP || !bytes.HasSuffix(first, []byte("hello")) {
		t.Errorf("first write = %q, want the request and the payload together", first)
	}
	upstream.Close()
}

func TestConnectSendsTheRequestAloneOnceMaxFirstPayloadDelayHasPassed(t *testing.T) {
	underlyingStream := newWriteRecordingStream()
	upstream, err := newClientWith(t, vless.Settings{UUID: aliceUUID, MaxFirstPayloadDelay: types.Pointer(types.Duration(30 * time.Millisecond))}).
		RequestStream(context.Background(), underlyingStream, spec.Metadata{Network: spec.NetworkTCP, Destination: domainDestination("ssh.example", 22)})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()

	select {
	case first := <-underlyingStream.writes:
		request, err := wire.ReadRequest(bytes.NewReader(first))
		if err != nil || request.Destination != domainDestination("ssh.example", 22) {
			t.Errorf("write = %q, %v, want exactly one request", first, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the request was never sent")
	}
}

func TestAClientRefusesUDPOnAStreamAndADestinationTheFormatCannotCarry(t *testing.T) {
	clientSide, _ := pipe(t)
	vlessClient := newClient(t, aliceUUID)

	if _, err := vlessClient.RequestStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP}); !errors.Is(err, spec.ErrUnsupportedNetwork) {
		t.Errorf("UDP on a stream: error = %v, want spec.ErrUnsupportedNetwork", err)
	}
	longDomain := domainDestination(strings.Repeat("a", wire.MaxDomainLength+1), 80)
	if _, err := vlessClient.RequestStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkTCP, Destination: longDomain}); !errors.Is(err, wire.ErrDomainTooLong) {
		t.Errorf("long domain: error = %v, want wire.ErrDomainTooLong", err)
	}
}

func TestTheClientSaysItWritesTheDatagramDestinationInTheStreamRequest(t *testing.T) {
	if got := newClient(t, aliceUUID).DatagramDestinationIsWritten(); got != spec.DatagramDestinationInStreamRequest {
		t.Errorf("DatagramDestinationIsWritten = %d, want spec.DatagramDestinationInStreamRequest", got)
	}
}

// What a client that is not VLESS, or not a user, sends is handed to the
// fallback; nothing has been written back.
func TestTheServerAsksForTheFallbackOnWhatIsNotAKnownUsersRequest(t *testing.T) {
	validFor := func(uuid string) []byte {
		parsed, _ := wire.ParseUUID(uuid)
		request, _ := wire.AppendRequest(nil, wire.Request{UUID: parsed, Command: wire.CommandTCP, Destination: domainDestination("example.com", 443)})
		return request
	}
	wrongVersion := validFor(aliceUUID)
	wrongVersion[0] = 1

	tests := []struct {
		name         string
		input        []byte
		wantAuthFail bool
	}{
		{"unknown uuid", validFor("c0ffee00-0000-4000-8000-000000000000"), true},
		{"another version", wrongVersion, false},
		// Long enough to hold the command byte wherever the addons length it
		// took from the text puts it. A shorter probe waits for its handshake
		// timeout and is closed, as with Trojan (an open question in CLAUDE.md).
		{"http", []byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: " + strings.Repeat("x", 300) + "\r\n\r\n"), false},
		{"tls client hello", append([]byte{0x16, 0x03, 0x01, 0x02, 0x00, 0x01, 0x00, 0x01, 0xfc, 0x03, 0x03}, make([]byte, 40)...), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clientSide, serverSide := pipe(t)
			results := serve(t, serverSide)
			go clientSide.Write(test.input)

			result := receive(t, results)
			if !errors.Is(result.err, spec.ErrFallbackRequested) {
				t.Fatalf("error = %v, want one asking for the fallback", result.err)
			}
			if errors.Is(result.err, spec.ErrAuthenticationFailed) != test.wantAuthFail {
				t.Errorf("error = %v, ErrAuthenticationFailed should be %v", result.err, test.wantAuthFail)
			}
		})
	}
}

func TestACutShortOrSilentClientIsNotHandedToTheFallback(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serve(t, serverSide)
	clientSide.Write([]byte{0x00, 0x01, 0x02})
	clientSide.Close()

	result := receive(t, results)
	if result.err == nil || errors.Is(result.err, spec.ErrFallbackRequested) {
		t.Errorf("error = %v, want a plain error: a closed connection is closed, not forwarded", result.err)
	}
}

func TestAServerRefusesAKnownUsersFlowAndMuxWithoutAskingForTheFallback(t *testing.T) {
	uuid, _ := wire.ParseUUID(aliceUUID)
	withFlow, _ := wire.AppendRequest(nil, wire.Request{UUID: uuid, Flow: "xtls-rprx-vision", Command: wire.CommandTCP, Destination: domainDestination("example.com", 443)})
	mux := append([]byte{0x00}, uuid[:]...)
	mux = append(mux, 0x00, wire.CommandMux)

	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{"flow", withFlow, vless.ErrUnsupportedFlow},
		{"mux", mux, vless.ErrUnsupportedCommand},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clientSide, serverSide := pipe(t)
			results := serve(t, serverSide)
			go clientSide.Write(test.input)

			result := receive(t, results)
			if !errors.Is(result.err, test.want) || errors.Is(result.err, spec.ErrFallbackRequested) {
				t.Errorf("error = %v, want %v and no fallback: the client is genuine", result.err, test.want)
			}
		})
	}
}

func TestAServerGivesUpOnASilentClientAtItsHandshakeTimeout(t *testing.T) {
	settings := serverSettings
	settings.HandshakeTimeout = types.Pointer(types.Duration(100 * time.Millisecond))
	server, err := (&vless.Proxy{Settings: settings}).NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	_, serverSide := pipe(t)
	result := make(chan error, 1)
	go func() {
		_, _, err := server.AcceptStream(context.Background(), serverSide)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Error("a silent client was accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a silent client held the handshake past handshake_timeout")
	}
}

func TestIdentifyingTellsUDPFromTCPEvenWithAddonsBeforeTheCommand(t *testing.T) {
	uuid, _ := wire.ParseUUID(aliceUUID)
	for _, test := range []struct {
		command byte
		flow    string
		want    bool
	}{
		{wire.CommandUDP, "", true},
		{wire.CommandTCP, "", false},
		{wire.CommandUDP, "some-flow", true},
		{wire.CommandTCP, "some-flow", false},
	} {
		request, err := wire.AppendRequest(nil, wire.Request{UUID: uuid, Flow: test.flow, Command: test.command, Destination: ipDestination("192.0.2.1", 53)})
		if err != nil {
			t.Fatal(err)
		}
		clientSide, serverSide := pipe(t)
		go clientSide.Write(request)

		isDatagram, err := newServer(t).IsDatagramOverStream(context.Background(), newBufferedStream(serverSide))
		if err != nil || isDatagram != test.want {
			t.Errorf("command %d flow %q: IsDatagramOverStream = %v, %v, want %v", test.command, test.flow, isDatagram, err, test.want)
		}
	}
}

func TestIdentifyingNeverConsumesTheRequest(t *testing.T) {
	uuid, _ := wire.ParseUUID(aliceUUID)
	request, _ := wire.AppendRequest(nil, wire.Request{UUID: uuid, Command: wire.CommandTCP, Destination: ipDestination("192.0.2.1", 80)})
	clientSide, serverSide := pipe(t)
	go clientSide.Write(request)

	peekable := newBufferedStream(serverSide)
	if _, err := newServer(t).IsDatagramOverStream(context.Background(), peekable); err != nil {
		t.Fatal(err)
	}
	read := make([]byte, len(request))
	if _, err := io.ReadFull(peekable, read); err != nil || !bytes.Equal(read, request) {
		t.Errorf("after identifying, the stream holds %q, %v, want the whole request still there", read, err)
	}
}

func TestAnAcceptedStreamHalfClosedBeforeWritingStillSendsTheAnswerHeader(t *testing.T) {
	underlying := &halfCloseRecordingStream{writes: make(chan []byte, 4)}
	uuid, _ := wire.ParseUUID(aliceUUID)
	request, _ := wire.AppendRequest(nil, wire.Request{UUID: uuid, Command: wire.CommandTCP, Destination: ipDestination("192.0.2.1", 80)})
	underlying.input = bytes.NewReader(request)

	stream, _, err := newServer(t).AcceptStream(context.Background(), underlying)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if header := <-underlying.writes; !bytes.Equal(header, []byte{0x00, 0x00}) {
		t.Errorf("write = % X, want the answer header 00 00", header)
	}
	if !underlying.halfClosed {
		t.Error("the underlying stream was not half-closed")
	}
}

func TestAnAcceptedStreamClosedBeforeWritingSendsNothing(t *testing.T) {
	underlying := &halfCloseRecordingStream{writes: make(chan []byte, 4)}
	uuid, _ := wire.ParseUUID(aliceUUID)
	request, _ := wire.AppendRequest(nil, wire.Request{UUID: uuid, Command: wire.CommandTCP, Destination: ipDestination("192.0.2.1", 80)})
	underlying.input = bytes.NewReader(request)

	stream, _, err := newServer(t).AcceptStream(context.Background(), underlying)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	select {
	case written := <-underlying.writes:
		t.Errorf("wrote % X when closed without data, want nothing: a destination that could not be reached looks like a refusal", written)
	default:
	}
}

// halfCloseRecordingStream is a server-side stream with a canned request.
type halfCloseRecordingStream struct {
	input      *bytes.Reader
	writes     chan []byte
	halfClosed bool
}

func (s *halfCloseRecordingStream) Read(p []byte) (int, error) { return s.input.Read(p) }
func (s *halfCloseRecordingStream) Write(p []byte) (int, error) {
	s.writes <- append([]byte(nil), p...)
	return len(p), nil
}
func (s *halfCloseRecordingStream) Close() error        { return nil }
func (s *halfCloseRecordingStream) CloseWrite() error   { s.halfClosed = true; return nil }
func (s *halfCloseRecordingStream) LocalAddr() net.Addr { return &net.TCPAddr{} }
func (s *halfCloseRecordingStream) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 9), Port: 5000}
}
func (s *halfCloseRecordingStream) SetDeadline(time.Time) error      { return nil }
func (s *halfCloseRecordingStream) SetReadDeadline(time.Time) error  { return nil }
func (s *halfCloseRecordingStream) SetWriteDeadline(time.Time) error { return nil }
