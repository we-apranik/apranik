package websocket

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
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

// connect runs both sides' WrapXStream at once over a loopback TCP pair.
func connect(t testing.TB, clientModule, serverModule *Transport) (client, server spec.Stream) {
	t.Helper()
	overlayClient, _ := clientModule.NewStreamOverlayClient()
	overlayServer, _ := serverModule.NewStreamOverlayServer()
	clientSide, serverSide := tcpPair(t)

	// Not cancelled on return: with early data the server's handshake is
	// still running when connect returns.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	serverDone := make(chan wrapResult, 1)
	go func() {
		stream, err := overlayServer.WrapServerStream(ctx, serverSide)
		serverDone <- wrapResult{stream, err}
	}()
	clientStream, err := overlayClient.WrapClientStream(ctx, clientSide, testServer(t))
	if err != nil {
		t.Fatalf("WrapClientStream: %v", err)
	}
	if clientModule.MaxEarlyData > 0 {
		// The client's handshake waits for its first Write, so the server
		// can only finish once the test writes; hand back a lazy result.
		return clientStream, &lazyServer{done: serverDone, t: t}
	}
	result := <-serverDone
	if result.err != nil {
		t.Fatalf("WrapServerStream: %v", result.err)
	}
	return clientStream, result.stream
}

// lazyServer waits for the server's handshake on first use.
type lazyServer struct {
	spec.Stream
	done chan wrapResult
	t    testing.TB
}

func (l *lazyServer) resolve() spec.Stream {
	if l.Stream == nil {
		select {
		case result := <-l.done:
			if result.err != nil {
				l.t.Fatalf("WrapServerStream: %v", result.err)
			}
			l.Stream = result.stream
		case <-time.After(5 * time.Second):
			l.t.Fatal("the server's handshake never finished")
		}
	}
	return l.Stream
}

func (l *lazyServer) Read(p []byte) (int, error)  { return l.resolve().Read(p) }
func (l *lazyServer) Write(p []byte) (int, error) { return l.resolve().Write(p) }

func TestBytesOfEverySizeCrossBothWays(t *testing.T) {
	client, server := connect(t, newModule(t, nil), newModule(t, nil))
	for _, size := range []int{1, 125, 126, 65535, 65536, 300_000} {
		payload := make([]byte, size)
		rand.Read(payload)
		for direction, pair := range map[string][2]spec.Stream{"client to server": {client, server}, "server to client": {server, client}} {
			go pair[0].Write(payload)
			received := make([]byte, size)
			if _, err := io.ReadFull(pair[1], received); err != nil || !bytes.Equal(received, payload) {
				t.Fatalf("%s, %d bytes: %v", direction, size, err)
			}
		}
	}
}

// rawRequest sends an upgrade request by hand and returns the server's
// response and a reader positioned after it.
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

const validKey = "dGhlIHNhbXBsZSBub25jZQ=="

func upgradeRequest(path string, extraLines string) string {
	return "GET " + path + " HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n" +
		"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + validKey + "\r\n" + extraLines + "\r\n"
}

func TestARefusedRequestGetsAnOrdinaryHTTPAnswer(t *testing.T) {
	serverModule := newModule(t, func(s *Settings) { s.Path = "/chat" })
	for name, tc := range map[string]struct {
		request string
		status  int
	}{
		"wrong path":     {upgradeRequest("/other", ""), http.StatusNotFound},
		"not an upgrade": {"GET /chat HTTP/1.1\r\nHost: example.com\r\n\r\n", http.StatusBadRequest},
		"wrong method":   {strings.Replace(upgradeRequest("/chat", ""), "GET", "POST", 1), http.StatusBadRequest},
		"no key":         {strings.Replace(upgradeRequest("/chat", ""), "Sec-WebSocket-Key: "+validKey+"\r\n", "", 1), http.StatusBadRequest},
		"old version":    {strings.Replace(upgradeRequest("/chat", ""), "Version: 13", "Version: 8", 1), http.StatusBadRequest},
		"not http":       {"\x16\x03\x01\x00\x05hello\r\n\r\n", http.StatusBadRequest},
	} {
		response, _, _, serverDone := rawRequest(t, serverModule, tc.request)
		if response.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", name, response.StatusCode, tc.status)
		}
		if result := <-serverDone; result.err == nil {
			t.Errorf("%s: the server accepted it", name)
		}
	}
}

func TestTheQueryIsNotPartOfThePath(t *testing.T) {
	response, _, _, serverDone := rawRequest(t, newModule(t, func(s *Settings) { s.Path = "/chat" }), upgradeRequest("/chat?ed=2048", ""))
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", response.StatusCode)
	}
	if response.Header.Get("Sec-WebSocket-Accept") != acceptKeyFor(validKey) {
		t.Error("wrong accept key")
	}
	if result := <-serverDone; result.err != nil {
		t.Fatal(result.err)
	}
}

// What an Xray client sends: early data, URL-safe base64 without padding, in
// Sec-WebSocket-Protocol. The server must replay it and echo the header.
func TestXrayStyleEarlyDataIsReplayedAndEchoed(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("first payload"))
	response, _, _, serverDone := rawRequest(t, newModule(t, nil), upgradeRequest("/", "Sec-WebSocket-Protocol: "+encoded+"\r\n"))
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", response.StatusCode)
	}
	if got := response.Header.Get("Sec-WebSocket-Protocol"); got != encoded {
		t.Errorf("echoed %q, want %q", got, encoded)
	}
	result := <-serverDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	received := make([]byte, len("first payload"))
	if _, err := io.ReadFull(result.stream, received); err != nil || string(received) != "first payload" {
		t.Errorf("read %q, %v", received, err)
	}
}

func TestEarlyDataFromOurClient(t *testing.T) {
	for name, edit := range map[string]func(*Settings){
		"header mode, all early": func(s *Settings) { s.MaxEarlyData = 64 },
		"header mode, split":     func(s *Settings) { s.MaxEarlyData = 4 },
		"sing-box path mode": func(s *Settings) {
			s.MaxEarlyData = 64
			s.EarlyDataHeaderName = ""
		},
	} {
		client, server := connect(t, newModule(t, edit), newModule(t, edit))
		if n, err := client.Write([]byte("hello world")); err != nil || n != len("hello world") {
			t.Fatalf("%s: Write %d, %v", name, n, err)
		}
		received := make([]byte, len("hello world"))
		if _, err := io.ReadFull(server, received); err != nil || string(received) != "hello world" {
			t.Errorf("%s: server read %q, %v", name, received, err)
		}
		go server.Write([]byte("reply"))
		reply := make([]byte, 5)
		if _, err := io.ReadFull(client, reply); err != nil || string(reply) != "reply" {
			t.Errorf("%s: client read %q, %v", name, reply, err)
		}
	}
}

// With early data, a Read first waits for the Write that sends the handshake.
func TestAReadBeforeTheFirstWriteWaitsForIt(t *testing.T) {
	edit := func(s *Settings) { s.MaxEarlyData = 64 }
	client, server := connect(t, newModule(t, edit), newModule(t, edit))

	replies := make(chan string, 1)
	go func() {
		reply := make([]byte, 4)
		io.ReadFull(client, reply)
		replies <- string(reply)
	}()
	time.Sleep(50 * time.Millisecond)
	client.Write([]byte("ping"))
	request := make([]byte, 4)
	if _, err := io.ReadFull(server, request); err != nil || string(request) != "ping" {
		t.Fatalf("server read %q, %v", request, err)
	}
	server.Write([]byte("pong"))
	select {
	case reply := <-replies:
		if reply != "pong" {
			t.Errorf("client read %q", reply)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting Read never returned")
	}
}

func TestClosingAClientWaitingForItsHandshakeEndsItsRead(t *testing.T) {
	edit := func(s *Settings) { s.MaxEarlyData = 64 }
	client, _ := connect(t, newModule(t, edit), newModule(t, edit))
	done := make(chan error, 1)
	go func() {
		_, err := client.Read(make([]byte, 1))
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	client.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Read succeeded after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read still waiting after Close")
	}
}

// acceptedByHand upgrades a connection with a hand-written client, so a test
// can send frames the module's client never would.
func acceptedByHand(t *testing.T) (spec.Stream, *net.TCPConn, *bufio.Reader) {
	t.Helper()
	response, reader, clientSide, serverDone := rawRequest(t, newModule(t, nil), upgradeRequest("/", ""))
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", response.StatusCode)
	}
	result := <-serverDone
	if result.err != nil {
		t.Fatal(result.err)
	}
	return result.stream, clientSide, reader
}

func maskedFrame(opcode byte, final bool, payload []byte) []byte {
	header := frameHeader{final: final, opcode: opcode, masked: true, maskKey: [4]byte{9, 8, 7, 6}, length: uint64(len(payload))}
	frame := appendFrameHeader(nil, header)
	start := len(frame)
	frame = append(frame, payload...)
	maskBytes(header.maskKey, 0, frame[start:])
	return frame
}

func TestAPingBetweenFragmentsIsAnsweredAndTheMessageIsReadWhole(t *testing.T) {
	server, clientSide, reader := acceptedByHand(t)
	var wire []byte
	wire = append(wire, maskedFrame(opcodeBinary, false, []byte("hello "))...)
	wire = append(wire, maskedFrame(opcodePing, true, []byte("are you there"))...)
	wire = append(wire, maskedFrame(opcodeContinuation, true, []byte("world"))...)
	clientSide.Write(wire)

	received := make([]byte, len("hello world"))
	if _, err := io.ReadFull(server, received); err != nil || string(received) != "hello world" {
		t.Fatalf("server read %q, %v", received, err)
	}
	pong, err := readFrameHeader(reader)
	if err != nil || pong.opcode != opcodePong || pong.masked {
		t.Fatalf("got %+v, %v; want an unmasked pong", pong, err)
	}
	payload := make([]byte, pong.length)
	io.ReadFull(reader, payload)
	if string(payload) != "are you there" {
		t.Errorf("pong carried %q", payload)
	}
}

func TestACloseFrameEndsTheStreamAndIsAnswered(t *testing.T) {
	server, clientSide, reader := acceptedByHand(t)
	clientSide.Write(maskedFrame(opcodeClose, true, []byte{0x03, 0xE8}))
	if _, err := server.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("Read after Close: %v, want EOF", err)
	}
	answer, err := readFrameHeader(reader)
	if err != nil || answer.opcode != opcodeClose {
		t.Fatalf("got %+v, %v; want a Close", answer, err)
	}
	if _, err := server.Write([]byte("late")); err == nil {
		t.Error("a Write after the Close was accepted")
	}
}

func TestAnUnmaskedClientFrameIsRefused(t *testing.T) {
	server, clientSide, _ := acceptedByHand(t)
	clientSide.Write(appendFrameHeader(nil, frameHeader{final: true, opcode: opcodeBinary, length: 0}))
	if _, err := server.Read(make([]byte, 1)); !errors.Is(err, errUnmaskedFromClient) {
		t.Errorf("Read: %v", err)
	}
}

func TestClientFramesAreMaskedAndServerFramesAreNot(t *testing.T) {
	overlayClient, _ := newModule(t, nil).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	go func() {
		reader := bufio.NewReader(serverSide)
		request, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		serverSide.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
			acceptKeyFor(request.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n"))
		header, _ := readFrameHeader(reader)
		serverSide.Write([]byte{byte(0x80) | opcodeBinary, 0x01, boolByte(header.masked)})
	}()
	client, err := overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
	if err != nil {
		t.Fatal(err)
	}
	client.Write([]byte("x"))
	answer := make([]byte, 1)
	if _, err := io.ReadFull(client, answer); err != nil || answer[0] != 1 {
		t.Errorf("the client's frame was not masked (%v, %v)", answer, err)
	}
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

func TestAWrongAcceptKeyFailsTheClient(t *testing.T) {
	overlayClient, _ := newModule(t, nil).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	go func() {
		http.ReadRequest(bufio.NewReader(serverSide))
		serverSide.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: wrong\r\n\r\n"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := overlayClient.WrapClientStream(ctx, clientSide, testServer(t)); !errors.Is(err, errWrongAcceptKey) {
		t.Errorf("WrapClientStream: %v", err)
	}
}

func TestTheClientSendsHostPathAndHeaders(t *testing.T) {
	overlayClient, _ := newModule(t, func(s *Settings) {
		s.Path = "/chat?x=1"
		s.Host = "cdn.example"
		s.Headers = map[string]string{"X-Test": "yes"}
	}).NewStreamOverlayClient()
	clientSide, serverSide := tcpPair(t)
	requests := make(chan *http.Request, 1)
	go func() {
		request, _ := http.ReadRequest(bufio.NewReader(serverSide))
		requests <- request
		serverSide.Close()
	}()
	overlayClient.WrapClientStream(context.Background(), clientSide, testServer(t))
	request := <-requests
	if request == nil || request.Host != "cdn.example" || request.URL.RequestURI() != "/chat?x=1" ||
		request.Header.Get("X-Test") != "yes" || request.Header.Get("User-Agent") == "" {
		t.Errorf("request %+v", request)
	}
}

func TestTheDefaultHostIsTheServersAddress(t *testing.T) {
	domain := func(host string, port uint16) spec.Destination {
		server, _ := spec.NewDestinationFromDomain(host, port)
		return server
	}
	address := func(ip string, port uint16) spec.Destination {
		server, _ := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
		return server
	}
	for want, server := range map[string]spec.Destination{
		"example.com":      domain("example.com", 443),
		"example.com:8443": domain("example.com", 8443),
		"192.0.2.1":        address("192.0.2.1", 80),
		"[2001:db8::1]":    address("2001:db8::1", 443),
		"[2001:db8::1]:81": address("2001:db8::1", 81),
	} {
		if got := hostHeaderFor(server); got != want {
			t.Errorf("Host for %s is %q, want %q", server, got, want)
		}
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

func TestAnEndlessHeaderIsCutOff(t *testing.T) {
	overlayServer, _ := newModule(t, nil).NewStreamOverlayServer()
	clientSide, serverSide := tcpPair(t)
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := overlayServer.WrapServerStream(ctx, serverSide)
		done <- err
	}()
	go func() {
		clientSide.Write([]byte("GET / HTTP/1.1\r\n"))
		line := []byte("X-Filler: " + strings.Repeat("a", 1000) + "\r\n")
		for range 100 {
			if _, err := clientSide.Write(line); err != nil {
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a 100 KB header was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still reading")
	}
}

func TestCloseWriteIsRefusedSoTheRelayClosesBothWays(t *testing.T) {
	client, _ := connect(t, newModule(t, nil), newModule(t, nil))
	if err := client.CloseWrite(); !errors.Is(err, spec.ErrNoHalfClose) {
		t.Errorf("CloseWrite: %v", err)
	}
}

func TestValidateRefusesMalformedSettings(t *testing.T) {
	for name, edit := range map[string]func(*Settings){
		"no leading slash":    func(s *Settings) { s.Path = "chat" },
		"space in path":       func(s *Settings) { s.Path = "/a b" },
		"negative early data": func(s *Settings) { s.MaxEarlyData = -1 },
		"too much early data": func(s *Settings) { s.MaxEarlyData = maxEarlyDataLimit + 1 },
		"bad header name":     func(s *Settings) { s.Headers = map[string]string{"X Bad": "1"} },
		"line break in value": func(s *Settings) { s.Headers = map[string]string{"X-A": "1\r\nX-B: 2"} },
		"owned header":        func(s *Settings) { s.Headers = map[string]string{"Host": "a"} },
		"bad early header":    func(s *Settings) { s.EarlyDataHeaderName = "a:b" },
		"early header also set": func(s *Settings) {
			s.MaxEarlyData = 10
			s.Headers = map[string]string{"sec-websocket-protocol": "x"}
		},
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
	if first.Info() != second.Info() || first.Info().Name != "ws" {
		t.Errorf("Info %v and %v", first.Info(), second.Info())
	}
}
