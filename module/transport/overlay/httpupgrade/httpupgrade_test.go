package httpupgrade

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

func tcpPair(t testing.TB) (clientSide, serverSide *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan *net.TCPConn, 1)
	go func() {
		connection, _ := listener.AcceptTCP()
		accepted <- connection
	}()
	clientSide, err = net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	serverSide = <-accepted
	if serverSide == nil {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() { clientSide.Close(); serverSide.Close() })
	return clientSide, serverSide
}

func newModule(t testing.TB, edit func(*Settings)) *Transport {
	t.Helper()
	transport := NewOverlayStreamTransportModule().(*Transport)
	if edit != nil {
		edit(&transport.Settings)
	}
	if err := transport.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return transport
}

func testServer(t testing.TB) spec.Destination {
	t.Helper()
	server, err := spec.NewDestinationFromDomain("example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

type wrapResult struct {
	stream spec.Stream
	err    error
}

// connect runs both sides at once over a loopback TCP pair. With pipelining
// the server finishes on its own, since the client does not wait for it.
func connect(t testing.TB, clientModule, serverModule *Transport) (client, server spec.Stream) {
	t.Helper()
	overlayClient, _ := clientModule.NewStreamOverlayClient()
	overlayServer, _ := serverModule.NewStreamOverlayServer()
	clientSide, serverSide := tcpPair(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	serverDone := make(chan wrapResult, 1)
	go func() {
		stream, err := overlayServer.WrapServerStream(ctx, serverSide)
		serverDone <- wrapResult{stream, err}
	}()
	client, err := overlayClient.WrapClientStream(ctx, clientSide, testServer(t))
	if err != nil {
		t.Fatalf("WrapClientStream: %v", err)
	}
	result := <-serverDone
	if result.err != nil {
		t.Fatalf("WrapServerStream: %v", result.err)
	}
	return client, result.stream
}

func roundTrip(t *testing.T, client, server spec.Stream) {
	t.Helper()
	payload := make([]byte, 300_000)
	rand.Read(payload)
	go client.Write(payload)
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(server, received); err != nil || string(received) != string(payload) {
		t.Fatalf("client to server: %v", err)
	}
	go server.Write([]byte("reply"))
	reply := make([]byte, 5)
	if _, err := io.ReadFull(client, reply); err != nil || string(reply) != "reply" {
		t.Fatalf("server to client: %q, %v", reply, err)
	}
}

func TestBytesCrossBothWays(t *testing.T) {
	for name, pipeline := range map[string]bool{"waiting for 101": false, "pipelining": true} {
		t.Run(name, func(t *testing.T) {
			edit := func(s *Settings) { s.Pipeline = pipeline }
			client, server := connect(t, newModule(t, edit), newModule(t, edit))
			roundTrip(t, client, server)
		})
	}
}

// requestFromClient captures what the module's client sends.
func requestFromClient(t *testing.T, edit func(*Settings)) (raw string) {
	t.Helper()
	overlayClient, _ := newModule(t, edit).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	captured := make(chan string, 1)
	go func() {
		serverSide.SetReadDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(serverSide)
		var request strings.Builder
		for {
			line, err := reader.ReadString('\n')
			request.WriteString(line)
			if err != nil || line == "\r\n" {
				break
			}
		}
		captured <- request.String()
		serverSide.Write([]byte(switchingProtocolsResponse))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := overlayClient.WrapClientStream(ctx, clientSide, testServer(t)); err != nil {
		t.Fatalf("WrapClientStream: %v", err)
	}
	return <-captured
}

// What Xray's and sing-box's servers check: exactly "Connection: Upgrade",
// "Upgrade: websocket", and (for sing-box) no WebSocket key.
func TestTheRequestIsWhatXrayAndSingBoxServersAccept(t *testing.T) {
	request := requestFromClient(t, func(s *Settings) {
		s.Path = "/up?x=1"
		s.Headers = map[string]string{"X-Test": "yes"}
	})
	for _, want := range []string{
		"GET /up?x=1 HTTP/1.1\r\n", "Host: example.com\r\n", "X-Test: yes\r\n", "User-Agent: Mozilla/",
		"Connection: Upgrade\r\n", "Upgrade: websocket\r\n",
	} {
		if !strings.Contains(request, want) {
			t.Errorf("request lacks %q:\n%s", want, request)
		}
	}
	if strings.Contains(strings.ToLower(request), "sec-websocket") {
		t.Errorf("request carries a WebSocket header:\n%s", request)
	}
	if request := requestFromClient(t, func(s *Settings) { s.Host = "cdn.example" }); !strings.Contains(request, "Host: cdn.example\r\n") {
		t.Errorf("host setting ignored:\n%s", request)
	}
}

// Xray's client compares the status line and the Connection value whole.
func TestTheResponseIsWhatXrayClientsAccept(t *testing.T) {
	response, _, _, serverDone := rawRequest(t, newModule(t, nil), upgradeRequest("/", ""))
	if response.Status != "101 Switching Protocols" || response.Header.Get("Connection") != "Upgrade" ||
		response.Header.Get("Upgrade") != "websocket" {
		t.Errorf("response %s %v", response.Status, response.Header)
	}
	<-serverDone
}

func rawRequest(t *testing.T, serverModule *Transport, request string) (*http.Response, *bufio.Reader, *net.TCPConn, <-chan wrapResult) {
	t.Helper()
	overlayServer, _ := serverModule.NewStreamOverlayServer()
	clientSide, serverSide := tcpPair(t)
	serverDone := make(chan wrapResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stream, err := overlayServer.WrapServerStream(ctx, serverSide)
		serverDone <- wrapResult{stream, err}
	}()
	clientSide.Write([]byte(request))
	clientSide.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(clientSide)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}
	return response, reader, clientSide, serverDone
}

func upgradeRequest(path, extraLines string) string {
	return "GET " + path + " HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n" + extraLines + "\r\n"
}

func TestRefusalsAnswerLikeSingBox(t *testing.T) {
	serverModule := newModule(t, func(s *Settings) {
		s.Path = "/up"
		s.Host = "example.com"
	})
	for name, tc := range map[string]struct {
		request string
		status  int
	}{
		"wrong host":    {strings.Replace(upgradeRequest("/up", ""), "example.com", "other.example", 1), http.StatusBadRequest},
		"wrong path":    {upgradeRequest("/other", ""), http.StatusNotFound},
		"wrong method":  {strings.Replace(upgradeRequest("/up", ""), "GET", "POST", 1), http.StatusNotFound},
		"no upgrade":    {"GET /up HTTP/1.1\r\nHost: example.com\r\n\r\n", http.StatusNotFound},
		"not websocket": {strings.Replace(upgradeRequest("/up", ""), "Upgrade: websocket", "Upgrade: h2c", 1), http.StatusNotFound},
		"malformed":     {"\x16\x03\x01\x00\x05hello\r\n\r\n", http.StatusBadRequest},
	} {
		response, _, _, serverDone := rawRequest(t, serverModule, tc.request)
		if response.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", name, response.StatusCode, tc.status)
		}
		if result := <-serverDone; result.err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLenientRequestsAreAccepted(t *testing.T) {
	for name, request := range map[string]string{
		"connection list":  strings.Replace(upgradeRequest("/", ""), "Connection: Upgrade", "Connection: keep-alive, Upgrade", 1),
		"query in path":    upgradeRequest("/?ed=2048", ""),
		"host with a port": strings.Replace(upgradeRequest("/", ""), "example.com", "example.com:8080", 1),
		"websocket key":    upgradeRequest("/", "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n"),
	} {
		response, _, _, serverDone := rawRequest(t, newModule(t, nil), request)
		if response.StatusCode != http.StatusSwitchingProtocols {
			t.Errorf("%s: status %d", name, response.StatusCode)
		}
		<-serverDone
	}
}

func TestAWebSocketKeyIsRefusedWhenConfiguredLikeSingBox(t *testing.T) {
	response, _, _, serverDone := rawRequest(t, newModule(t, func(s *Settings) { s.AcceptWebSocketKey = false }),
		upgradeRequest("/", "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"))
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", response.StatusCode)
	}
	if result := <-serverDone; !errors.Is(result.err, errWebSocketKeyRefused) {
		t.Errorf("server: %v", result.err)
	}
}

// A pipelining client's first payload may arrive in the same read as its
// request. Xray's and sing-box's servers can drop it; this one must not.
func TestPayloadSentWithTheRequestIsKept(t *testing.T) {
	overlayServer, _ := newModule(t, nil).NewStreamOverlayServer()
	clientSide, serverSide := tcpPair(t)
	clientSide.Write([]byte(upgradeRequest("/", "") + "payload behind the request"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server, err := overlayServer.WrapServerStream(ctx, serverSide)
	if err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len("payload behind the request"))
	if _, err := io.ReadFull(server, received); err != nil || string(received) != "payload behind the request" {
		t.Errorf("read %q, %v", received, err)
	}
}

// And the other way: data the server sends with its 101 reaches the client.
func TestPayloadSentWithTheResponseIsKept(t *testing.T) {
	for name, pipeline := range map[string]bool{"waiting": false, "pipelining": true} {
		overlayClient, _ := newModule(t, func(s *Settings) { s.Pipeline = pipeline }).NewStreamOverlayClient()
		clientSide, serverSide := tcpPair(t)
		go func() {
			http.ReadRequest(bufio.NewReader(serverSide))
			serverSide.Write([]byte(switchingProtocolsResponse + "hello"))
		}()
		client, err := overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		received := make([]byte, 5)
		if _, err := io.ReadFull(client, received); err != nil || string(received) != "hello" {
			t.Errorf("%s: read %q, %v", name, received, err)
		}
	}
}

// With pipelining the client writes before the 101 arrives.
func TestAPipeliningClientWritesBeforeTheResponse(t *testing.T) {
	overlayClient, _ := newModule(t, func(s *Settings) { s.Pipeline = true }).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	client, err := overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
	if err != nil {
		t.Fatal(err)
	}
	client.Write([]byte("early"))

	serverSide.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(serverSide)
	if _, err := http.ReadRequest(reader); err != nil {
		t.Fatal(err)
	}
	early := make([]byte, 5)
	if _, err := io.ReadFull(reader, early); err != nil || string(early) != "early" {
		t.Fatalf("server read %q before answering, %v", early, err)
	}
}

func TestAPipeliningClientReportsARefusalOnItsFirstRead(t *testing.T) {
	overlayClient, _ := newModule(t, func(s *Settings) { s.Pipeline = true }).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	go func() {
		http.ReadRequest(bufio.NewReader(serverSide))
		serverSide.Write([]byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"))
	}()
	client, err := overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, errUpgradeRefused) {
		t.Errorf("Read: %v", err)
	}
}

func TestARefusalFailsAWaitingClient(t *testing.T) {
	overlayClient, _ := newModule(t, nil).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	go func() {
		http.ReadRequest(bufio.NewReader(serverSide))
		serverSide.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := overlayClient.WrapClientStream(ctx, clientSide, testServer(t)); !errors.Is(err, errUpgradeRefused) {
		t.Errorf("WrapClientStream: %v", err)
	}
}

func TestHalfClosePassesThrough(t *testing.T) {
	client, server := connect(t, newModule(t, nil), newModule(t, nil))
	client.Write([]byte("last"))
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	everything, err := io.ReadAll(server)
	if err != nil || string(everything) != "last" {
		t.Fatalf("server read %q, %v", everything, err)
	}
	go server.Write([]byte("reply"))
	reply := make([]byte, 5)
	if _, err := io.ReadFull(client, reply); err != nil || string(reply) != "reply" {
		t.Errorf("client read %q, %v after its own CloseWrite", reply, err)
	}
}

func TestACancelledContextEndsAHandshakeWithASilentClient(t *testing.T) {
	overlayServer, _ := newModule(t, nil).NewStreamOverlayServer()
	_, serverSide := tcpPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := overlayServer.WrapServerStream(ctx, serverSide)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a silent client completed a handshake")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handshake outlived its context")
	}
}

func TestValidateRefusesMalformedSettings(t *testing.T) {
	for name, edit := range map[string]func(*Settings){
		"no leading slash": func(s *Settings) { s.Path = "up" },
		"bad host":         func(s *Settings) { s.Host = "a/b" },
		"owned header":     func(s *Settings) { s.Headers = map[string]string{"Connection": "close"} },
		"bad header name":  func(s *Settings) { s.Headers = map[string]string{"X Bad": "1"} },
	} {
		transport := NewOverlayStreamTransportModule().(*Transport)
		edit(&transport.Settings)
		if transport.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestInfoIsTheSameForEveryValue(t *testing.T) {
	first, second := NewOverlayStreamTransportModule(), NewOverlayStreamTransportModule()
	if first.Info() != second.Info() || first.Info().Name != "httpupgrade" {
		t.Errorf("Info %v and %v", first.Info(), second.Info())
	}
}

// After the handshake the stream is the stream below: no allocation per
// write or read.
func BenchmarkClientToServer16KiB(b *testing.B) {
	client, server := connect(b, newModule(b, nil), newModule(b, nil))
	payload := make([]byte, 16<<10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 32<<10)
		for total := 0; total < b.N*len(payload); {
			n, err := server.Read(buffer)
			if err != nil {
				return
			}
			total += n
		}
	}()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
	<-done
}

func BenchmarkBareTCP16KiB(b *testing.B) {
	client, server := tcpPair(b)
	payload := make([]byte, 16<<10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 32<<10)
		for total := 0; total < b.N*len(payload); {
			n, err := server.Read(buffer)
			if err != nil {
				return
			}
			total += n
		}
	}()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
	<-done
}

// The upgrade request is HTTP/1.1, so the config check refuses a TLS below
// that offers anything else.
func TestItSpeaksHTTP1Point1(t *testing.T) {
	provider, provides := NewOverlayStreamTransportModule().(spec.TransportInfoProvider)
	if !provides {
		t.Fatal("no TransportInfo")
	}
	if got := provider.TransportInfo().ApplicationProtocols; len(got) != 1 || got[0] != "http/1.1" {
		t.Errorf("speaks %v, want [http/1.1]", got)
	}
}

// A failed read of the pending 101 may have consumed part of it, so a retry
// would parse garbage; every later Read reports the first failure instead.
func TestAPipeliningClientKeepsReportingAFailedResponse(t *testing.T) {
	overlayClient, _ := newModule(t, func(s *Settings) { s.Pipeline = true }).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	go func() {
		http.ReadRequest(bufio.NewReader(serverSide))
		serverSide.Write([]byte("HTTP/1.1 101 Swi"))
	}()
	client, err := overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
	if err != nil {
		t.Fatal(err)
	}
	client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, firstErr := client.Read(make([]byte, 1))
	if !errors.Is(firstErr, os.ErrDeadlineExceeded) {
		t.Fatalf("first Read: %v, want the deadline", firstErr)
	}
	serverSide.Write([]byte("tching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\npayload"))
	client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 7)); err != firstErr {
		t.Errorf("second Read: %v, want the first failure again", err)
	}
}

// A read deadline set while the client waits for the 101 bounds that wait at
// once, as it would a read on the stream below.
func TestAReadDeadlineSetDuringTheResponseWaitApplies(t *testing.T) {
	overlayClient, _ := newModule(t, func(s *Settings) { s.Pipeline = true }).NewStreamOverlayClient()
	clientSide, _ := tcpPair(t)
	client, err := overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := client.Read(make([]byte, 1))
		readDone <- err
	}()
	time.Sleep(50 * time.Millisecond)
	client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	select {
	case err := <-readDone:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("Read: %v, want the deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the new read deadline did not end the wait for the 101")
	}
}

// The response deadline ends with the response; a caller's earlier read
// deadline outlives it.
func TestTheCallerReadDeadlineOutlivesTheResponse(t *testing.T) {
	overlayClient, _ := newModule(t, func(s *Settings) { s.Pipeline = true }).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	go func() {
		http.ReadRequest(bufio.NewReader(serverSide))
		serverSide.Write([]byte(switchingProtocolsResponse))
	}()
	client, err := overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
	if err != nil {
		t.Fatal(err)
	}
	client.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	started := time.Now()
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Read: %v, want the caller's deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("Read waited %s, want about 300ms", elapsed)
	}
}
