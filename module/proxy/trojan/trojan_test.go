// Package trojan_test exercises the trojan module through spec alone: no
// registry, no builder, no relay. The carrier is a net.Pipe.
package trojan_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/proxy/trojan"
	"github.com/The-Apranik/Apranik/module/proxy/trojan/wire"
	"github.com/The-Apranik/Apranik/spec"
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
// Trojan never asks for it.
func (bufferedStream) PeekExactly(int) ([]byte, error) { panic("trojan does not use PeekExactly") }
func newBufferedStream(s spec.Stream) bufferedStream   { return bufferedStream{s, bufio.NewReader(s)} }

func pipe(t *testing.T) (clientSide, serverSide spec.Stream) {
	t.Helper()
	x, y := net.Pipe()
	t.Cleanup(func() { x.Close(); y.Close() })
	return pipeStream{x}, pipeStream{y}
}

var serverSettings = trojan.Settings{Users: []trojan.User{
	{Name: "alice", Password: "alice-password"},
	{Name: "bob", Password: "bob-password"},
}}

func newServer(t *testing.T) interface {
	spec.StreamProxyServer
	spec.DatagramOverStreamProxyServer
} {
	t.Helper()
	proxy := &trojan.Proxy{Settings: serverSettings}
	streamServer, err := proxy.NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	return streamServer.(interface {
		spec.StreamProxyServer
		spec.DatagramOverStreamProxyServer
	})
}

func newClient(t *testing.T, password types.Secret) interface {
	spec.StreamProxyClient
	spec.DatagramOverStreamProxyClient
} {
	t.Helper()
	proxy := &trojan.Proxy{Settings: trojan.Settings{Password: password}}
	streamClient, err := proxy.NewStreamClient()
	if err != nil {
		t.Fatal(err)
	}
	return streamClient.(interface {
		spec.StreamProxyClient
		spec.DatagramOverStreamProxyClient
	})
}

func domainDestination(domain string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		panic(err)
	}
	return destination
}

type acceptedStream struct {
	isDatagram bool
	payload    spec.Stream
	metadata   spec.Metadata
	err        error
}

// serveStream does what core does: identify, then call the matching accept.
func serveStream(t *testing.T, carrier spec.Stream) <-chan acceptedStream {
	server := newServer(t)
	results := make(chan acceptedStream, 1)
	go func() {
		peekable := newBufferedStream(carrier)
		isDatagram, err := server.IsDatagramOverStream(context.Background(), peekable)
		if err != nil || isDatagram {
			results <- acceptedStream{isDatagram: isDatagram, err: err}
			return
		}
		payload, metadata, err := server.AcceptStream(context.Background(), peekable)
		results <- acceptedStream{payload: payload, metadata: metadata, err: err}
	}()
	return results
}

func TestConnectRoundTripKeepsEarlyPayload(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serveStream(t, serverSide)

	destination := domainDestination("example.com", 443)
	upstream, err := newClient(t, "bob-password").RequestStream(context.Background(), clientSide, spec.Metadata{
		Network: spec.NetworkTCP, Destination: destination,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Written straight after the header, so it may share its segment.
	go upstream.Write([]byte("early payload"))

	got := <-results
	if got.err != nil {
		t.Fatalf("accept: %v", got.err)
	}
	if got.metadata.Destination != destination || got.metadata.Network != spec.NetworkTCP {
		t.Errorf("metadata = %+v", got.metadata)
	}
	if got.metadata.User != "bob" {
		t.Errorf("User = %q, want bob", got.metadata.User)
	}

	buffer := make([]byte, len("early payload"))
	if _, err := io.ReadFull(got.payload, buffer); err != nil || string(buffer) != "early payload" {
		t.Errorf("payload = %q, %v", buffer, err)
	}
}

func TestUDPRoundTripBothDirections(t *testing.T) {
	clientSide, serverSide := pipe(t)
	server := newServer(t)

	type acceptedDatagram struct {
		datagram spec.Datagram
		metadata spec.Metadata
		err      error
	}
	results := make(chan acceptedDatagram, 1)
	go func() {
		peekable := newBufferedStream(serverSide)
		isDatagram, err := server.IsDatagramOverStream(context.Background(), peekable)
		if err != nil || !isDatagram {
			results <- acceptedDatagram{err: fmt.Errorf("IsDatagramOverStream = %v, %v; want true", isDatagram, err)}
			return
		}
		datagram, metadata, err := server.AcceptDatagramOverStream(context.Background(), peekable)
		results <- acceptedDatagram{datagram, metadata, err}
	}()

	clientDatagram, err := newClient(t, "alice-password").RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	dns := domainDestination("dns.example", 53)
	go clientDatagram.WriteTo([]byte("query"), dns)

	got := <-results
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.metadata.Network != spec.NetworkUDP || got.metadata.User != "alice" {
		t.Errorf("metadata = %+v", got.metadata)
	}

	buffer := make([]byte, 1500)
	n, destination, err := got.datagram.ReadFrom(buffer)
	if err != nil || string(buffer[:n]) != "query" || destination != dns {
		t.Fatalf("server read %q to %s, %v", buffer[:n], destination, err)
	}

	go got.datagram.WriteTo([]byte("answer"), dns)
	n, source, err := clientDatagram.ReadFrom(buffer)
	if err != nil || string(buffer[:n]) != "answer" || source != dns {
		t.Errorf("client read %q from %s, %v", buffer[:n], source, err)
	}
}

// Nothing is written back before refusing, so core can hand every byte to a
// fallback as if the proxy were never there.
func TestRefusalsAskForTheFallback(t *testing.T) {
	validHeader := func(password string) []byte {
		header, _ := wire.AppendRequest(nil, wire.Request{
			PasswordHash: wire.HashPassword(password), Command: wire.CommandConnect,
			Destination: domainDestination("example.com", 443),
		})
		return header
	}

	for _, tc := range []struct {
		name  string
		bytes []byte
	}{
		{"wrong password", validHeader("wrong")},
		{"HTTP probe", []byte("GET / HTTP/1.1\r\nHost: example.com\r\nUser-Agent: probe/1.0 (compatible)\r\n\r\n")},
		{"unknown address type", func() []byte { h := validHeader("alice-password"); h[wire.PasswordHashLength+3] = 0x07; return h }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientSide, serverSide := pipe(t)
			results := serveStream(t, serverSide)
			go clientSide.Write(tc.bytes)

			got := <-results
			if !errors.Is(got.err, spec.ErrFallbackRequested) {
				t.Errorf("err = %v, want spec.ErrFallbackRequested", got.err)
			}
		})
	}
}

func TestAClosedConnectionIsNotAFallback(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serveStream(t, serverSide)

	header, _ := wire.AppendRequest(nil, wire.Request{
		PasswordHash: wire.HashPassword("alice-password"), Command: wire.CommandConnect,
		Destination: domainDestination("example.com", 443),
	})
	go func() {
		clientSide.Write(header[:wire.PasswordHashLength+10])
		clientSide.Close()
	}()

	got := <-results
	if got.err == nil || errors.Is(got.err, spec.ErrFallbackRequested) {
		t.Errorf("err = %v, want a plain error: a truncated request is closed", got.err)
	}
}

func TestRefusalTextCarriesNoCredential(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serveStream(t, serverSide)

	hash := wire.HashPassword("wrong")
	header, _ := wire.AppendRequest(nil, wire.Request{PasswordHash: hash, Command: wire.CommandConnect, Destination: domainDestination("a.example", 1)})
	go clientSide.Write(header)

	got := <-results
	if got.err == nil || strings.Contains(got.err.Error(), string(hash[:8])) || strings.Contains(got.err.Error(), "wrong") {
		t.Errorf("err = %v", got.err)
	}
}

func TestClientRefusesUDPOnTheStreamPath(t *testing.T) {
	clientSide, _ := pipe(t)
	_, err := newClient(t, "p").RequestStream(context.Background(), clientSide, spec.Metadata{
		Network: spec.NetworkUDP, Destination: domainDestination("a.example", 53),
	})
	if !errors.Is(err, spec.ErrUnsupportedNetwork) {
		t.Errorf("err = %v, want spec.ErrUnsupportedNetwork", err)
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings trojan.Settings
		valid    bool
	}{
		{"outbound", trojan.Settings{Password: "p"}, true},
		{"inbound", serverSettings, true},
		{"nothing", trojan.Settings{}, false},
		{"user without name", trojan.Settings{Users: []trojan.User{{Password: "p"}}}, false},
		{"user without password", trojan.Settings{Users: []trojan.User{{Name: "a"}}}, false},
		{"duplicate name", trojan.Settings{Users: []trojan.User{{"a", "p"}, {"a", "q"}}}, false},
		{"duplicate password", trojan.Settings{Users: []trojan.User{{"a", "p"}, {"b", "p"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.settings.Validate()
			if (err == nil) != tc.valid {
				t.Errorf("Validate = %v, valid %v", err, tc.valid)
			}
			if err != nil && strings.Contains(err.Error(), "p\"") {
				t.Errorf("error names a password: %v", err)
			}
		})
	}
}

func TestSideWithoutItsCredentialIsRefusedAtBuild(t *testing.T) {
	if _, err := (&trojan.Proxy{Settings: trojan.Settings{Password: "p"}}).NewStreamServer(); err == nil {
		t.Error("a server with no users was built")
	}
	if _, err := (&trojan.Proxy{Settings: serverSettings}).NewStreamClient(); err == nil {
		t.Error("a client with no password was built")
	}
}

func TestSettingsDoNotPrintPasswords(t *testing.T) {
	for _, rendered := range []string{fmt.Sprintf("%v", serverSettings), fmt.Sprintf("%+v", serverSettings), fmt.Sprintf("%#v", serverSettings)} {
		if strings.Contains(rendered, "alice-password") {
			t.Errorf("settings leaked a password: %s", rendered)
		}
	}
}

func TestModuleFillsBothStreamCarriedShapes(t *testing.T) {
	var module any = &trojan.Proxy{}
	if _, ok := module.(spec.StreamProxy); !ok {
		t.Error("not a spec.StreamProxy")
	}
	if _, ok := module.(spec.DatagramOverStreamProxy); !ok {
		t.Error("not a spec.DatagramOverStreamProxy")
	}
	if _, ok := module.(spec.DatagramProxy); ok {
		t.Error("claims native datagrams; Trojan's UDP is framed in the stream")
	}
}

func TestDecodeFillsUsers(t *testing.T) {
	module := trojan.NewStreamProxyModule()
	decoder := json.NewDecoder(strings.NewReader(`{"users":[{"name":"alice","password":"hunter2"}]}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(module); err != nil {
		t.Fatal(err)
	}
	users := module.(*trojan.Proxy).Users
	if len(users) != 1 || users[0].Name != "alice" || users[0].Password.Reveal() != "hunter2" {
		t.Errorf("decoded %+v", users)
	}
}

func TestInfoIsConstantAcrossSettingsAndKinds(t *testing.T) {
	if trojan.NewStreamProxyModule().Info() != trojan.NewDatagramOverStreamProxyModule().Info() ||
		(&trojan.Proxy{}).Info() != (&trojan.Proxy{Settings: serverSettings}).Info() {
		t.Error("Info varies")
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

func expectNoWrite(t *testing.T, stream writeRecordingStream, within time.Duration) {
	t.Helper()
	select {
	case written := <-stream.writes:
		t.Fatalf("wrote %d bytes, want nothing yet", len(written))
	case <-time.After(within):
	}
}

func TestConnectSendsTheRequestWithTheFirstPayload(t *testing.T) {
	carrier := newWriteRecordingStream()
	destination := domainDestination("example.com", 443)
	upstream, err := newClient(t, "p").RequestStream(context.Background(), carrier, spec.Metadata{Network: spec.NetworkTCP, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	expectNoWrite(t, carrier, 20*time.Millisecond)

	upstream.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
	first := <-carrier.writes
	request, err := wire.ReadRequest(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("first write is not a request: %v", err)
	}
	if request.Destination != destination || !bytes.HasSuffix(first, []byte("GET / HTTP/1.1\r\n\r\n")) {
		t.Errorf("first write = %q, want the request and the payload together", first)
	}

	upstream.Write([]byte("more"))
	if second := <-carrier.writes; string(second) != "more" {
		t.Errorf("second write = %q, want the payload alone", second)
	}
	upstream.Close()
}

// A server-first protocol sends nothing, so the request cannot wait forever.
func TestConnectSendsTheRequestAloneWhenNoPayloadComes(t *testing.T) {
	carrier := newWriteRecordingStream()
	upstream, err := newClient(t, "p").RequestStream(context.Background(), carrier, spec.Metadata{
		Network: spec.NetworkTCP, Destination: domainDestination("smtp.example", 25),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()

	select {
	case first := <-carrier.writes:
		if length, err := wire.RequestLength(first); err != nil || length != len(first) {
			t.Errorf("write = %q, want exactly one request", first)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the request was never sent")
	}

	upstream.Write([]byte("EHLO"))
	if next := <-carrier.writes; string(next) != "EHLO" {
		t.Errorf("after the request = %q, want the payload alone", next)
	}
}

func TestUDPSendsTheRequestWithTheFirstDatagram(t *testing.T) {
	carrier := newWriteRecordingStream()
	datagram, err := newClient(t, "p").RequestDatagramOverStream(context.Background(), carrier, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	expectNoWrite(t, carrier, 150*time.Millisecond)

	dns := domainDestination("dns.example", 53)
	datagram.WriteTo([]byte("query"), dns)
	first := bytes.NewReader(<-carrier.writes)
	request, err := wire.ReadRequest(first)
	if err != nil {
		t.Fatalf("first write does not start with a request: %v", err)
	}
	if request.Command != wire.CommandUDPAssociate || request.Destination != dns {
		t.Errorf("request = command X'%02X' to %s, want UDP to %s", request.Command, request.Destination, dns)
	}
	buffer := make([]byte, 64)
	frames := bufio.NewReader(first)
	n, to, err := wire.ReadUDPFrame(frames, buffer)
	if err != nil || to != dns || string(buffer[:n]) != "query" || first.Len()+frames.Buffered() != 0 {
		t.Errorf("frame after the request = %q to %s, %v", buffer[:n], to, err)
	}

	datagram.WriteTo([]byte("again"), dns)
	if _, err := wire.ReadRequest(bytes.NewReader(<-carrier.writes)); err == nil {
		t.Error("the request was sent twice")
	}
}
