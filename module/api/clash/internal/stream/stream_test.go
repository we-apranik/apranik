package stream_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
)

const tick = 10 * time.Millisecond

// serveCounting serves a stream that sends {"n":1}, {"n":2}... every tick, and
// counts the handlers that returned.
func serveCounting(t *testing.T, options stream.Options) (*httptest.Server, *stream.Hub, *atomic.Int32) {
	t.Helper()
	if options.Tick == 0 {
		options.Tick = tick
	}
	hub := stream.NewHub(options)
	returned := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer returned.Add(1)
		hub.Serve(w, r, func(messages *stream.Stream) {
			count := 0
			messages.Every(func() ([]byte, bool) {
				count++
				return []byte(`{"n":` + string(rune('0'+count%10)) + `}`), true
			})
		})
	}))
	t.Cleanup(func() {
		hub.Close()
		server.Close()
		hub.Wait()
	})
	return server, hub, returned
}

func dial(t *testing.T, server *httptest.Server, origin string) (*websocket.Conn, error) {
	t.Helper()
	if origin == "" {
		origin = server.URL
	}
	connection, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", origin)
	if err == nil {
		t.Cleanup(func() { connection.Close() })
	}
	return connection, err
}

// rawUpgrade opens a TCP connection to the server and does the WebSocket handshake
// by hand, so the test can drop it without a close frame.
func rawUpgrade(t *testing.T, server *httptest.Server, extraHeaders string) (net.Conn, *http.Response, *bufio.Reader) {
	t.Helper()
	address := strings.TrimPrefix(server.URL, "http://")
	raw, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	raw.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(raw, "GET / HTTP/1.1\r\nHost: "+address+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n"+extraHeaders+"\r\n")
	reader := bufio.NewReader(raw)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	return raw, response, reader
}

func receive(connection *websocket.Conn) (string, error) {
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message string
	err := websocket.Message.Receive(connection, &message)
	return message, err
}

func TestAPlainRequestGetsOneJSONLinePerTick(t *testing.T) {
	server, _, _ := serveCounting(t, stream.Options{})
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("%d %v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	for want := 1; want <= 3; want++ {
		line, err := reader.ReadString('\n')
		if err != nil || line != `{"n":`+string(rune('0'+want))+"}\n" {
			t.Fatalf("line %d = %q, %v", want, line, err)
		}
	}
}

func TestAWebSocketGetsOneTextMessagePerTick(t *testing.T) {
	server, _, _ := serveCounting(t, stream.Options{})
	connection, err := dial(t, server, "")
	if err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 3; want++ {
		message, err := receive(connection)
		if err != nil || message != `{"n":`+string(rune('0'+want))+"}" {
			t.Fatalf("message %d = %q, %v", want, message, err)
		}
	}
}

func TestTheHandlerReturnsWhenAPlainClientLeaves(t *testing.T) {
	server, _, returned := serveCounting(t, stream.Options{})
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	bufio.NewReader(response.Body).ReadString('\n')
	response.Body.Close()
	eventually(t, "the handler returning", func() bool { return returned.Load() == 1 })
}

func TestTheHandlerReturnsWhenAWebSocketClientLeaves(t *testing.T) {
	server, _, returned := serveCounting(t, stream.Options{})
	connection, err := dial(t, server, "")
	if err != nil {
		t.Fatal(err)
	}
	receive(connection)
	connection.Close()
	eventually(t, "the handler returning", func() bool { return returned.Load() == 1 })
}

func TestTheHandlerReturnsWhenAWebSocketClientVanishes(t *testing.T) {
	server, _, returned := serveCounting(t, stream.Options{})
	raw, response, _ := rawUpgrade(t, server, "")
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake = %d", response.StatusCode)
	}
	// Dropped with no close frame, as a phone going out of range does.
	raw.Close()
	eventually(t, "the handler returning", func() bool { return returned.Load() == 1 })
}

func TestAWebSocketClientsPingIsAnswered(t *testing.T) {
	hub := stream.NewHub(stream.Options{Tick: time.Hour})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.Serve(w, r, func(messages *stream.Stream) { <-messages.Context().Done() })
	}))
	t.Cleanup(func() { hub.Close(); server.Close(); hub.Wait() })

	raw, response, reader := rawUpgrade(t, server, "")
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake = %d", response.StatusCode)
	}
	// A masked ping with a four byte payload.
	mask := []byte{1, 2, 3, 4}
	ping := append([]byte{0x89, 0x80 | 4}, mask...)
	for index, b := range []byte("ping") {
		ping = append(ping, b^mask[index])
	}
	raw.Write(ping)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 0x8A || header[1] != 4 {
		t.Fatalf("answer header % x, %v; want a pong with the same payload", header, err)
	}
	payload := make([]byte, 4)
	io.ReadFull(reader, payload)
	if string(payload) != "ping" {
		t.Errorf("pong payload %q", payload)
	}
}

func TestClosingTheHubEndsEveryStreamTellsTheWebSocketAndWaitReturns(t *testing.T) {
	server, hub, returned := serveCounting(t, stream.Options{Tick: time.Hour})
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	connection, err := dial(t, server, "")
	if err != nil {
		t.Fatal(err)
	}

	hub.Close()
	done := make(chan struct{})
	go func() { hub.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return while streams were open")
	}
	if returned.Load() != 2 {
		t.Errorf("%d handlers returned, want 2", returned.Load())
	}
	// The client was told: a close frame, which the library reports as an end of
	// the stream.
	if _, err := receive(connection); err == nil {
		t.Error("the WebSocket was still open")
	}
	if _, err := bufio.NewReader(response.Body).ReadString('\n'); err == nil {
		t.Error("the plain stream was still open")
	}
}

func TestAClosedHubRefusesNewStreamsWith503(t *testing.T) {
	server, hub, _ := serveCounting(t, stream.Options{})
	hub.Close()
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", response.StatusCode)
	}
	if end, ok := hub.Track(); ok || end != nil {
		t.Error("a closed hub tracked a new stream")
	}
}

func TestAWebSocketFromAnotherOriginIsRefusedUnlessTheOriginIsAllowed(t *testing.T) {
	cases := map[string]struct {
		allowed []string
		origin  string // empty: this server's own
		accept  bool
	}{
		"this host's own page":        {nil, "", true},
		"a page not allowed":          {nil, "https://dash.example", false},
		"the page that is allowed":    {[]string{"https://dash.example"}, "https://dash.example", true},
		"the same page, other case":   {[]string{"https://Dash.Example"}, "https://dash.example", true},
		"another page":                {[]string{"https://dash.example"}, "https://evil.example", false},
		"the same host, other scheme": {[]string{"https://dash.example"}, "http://dash.example", false},
		"an allowed host and port":    {[]string{"http://127.0.0.1:3000"}, "http://127.0.0.1:3000", true},
		"any page":                    {[]string{"*"}, "https://anything.example", true},
	}
	for name, test := range cases {
		server, _, returned := serveCounting(t, stream.Options{AllowedOrigins: test.allowed})
		connection, err := dial(t, server, test.origin)
		if test.accept {
			if err != nil {
				t.Errorf("%s: refused: %v", name, err)
			} else {
				connection.Close()
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		eventually(t, name+": the handler returning after a refused handshake", func() bool { return returned.Load() >= 1 })
	}
}

func TestAWebSocketWithoutAnOriginHeaderIsAccepted(t *testing.T) {
	server, _, _ := serveCounting(t, stream.Options{AllowedOrigins: []string{"https://dash.example"}})
	if _, response, _ := rawUpgrade(t, server, ""); response.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("a client without an Origin = %d; want 101", response.StatusCode)
	}
}

func TestAnUnsupportedVersionIsRefusedWith400(t *testing.T) {
	server, _, returned := serveCounting(t, stream.Options{})
	address := strings.TrimPrefix(server.URL, "http://")
	raw, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(raw, "GET / HTTP/1.1\r\nHost: "+address+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 8\r\n\r\n")
	if response, err := http.ReadResponse(bufio.NewReader(raw), nil); err != nil || response.StatusCode != http.StatusBadRequest {
		t.Errorf("version 8 = %v, %v; want 400", response, err)
	}
	eventually(t, "the handler returning", func() bool { return returned.Load() == 1 })
}

func TestSendReportsAClientThatWentAway(t *testing.T) {
	hub := stream.NewHub(stream.Options{Tick: time.Hour})
	sendFailed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.Serve(w, r, func(messages *stream.Stream) {
			<-messages.Context().Done()
			sendFailed <- messages.Send([]byte(`{}`))
		})
	}))
	t.Cleanup(func() { hub.Close(); server.Close(); hub.Wait() })

	raw, response, _ := rawUpgrade(t, server, "")
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake = %d", response.StatusCode)
	}
	raw.Close()
	select {
	case err := <-sendFailed:
		if err == nil {
			t.Log("Send to a client that left did not fail (the close was not yet seen by the kernel)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never noticed the client left")
	}
}

func TestBindEndsAContextWhenTheHubCloses(t *testing.T) {
	hub := stream.NewHub(stream.Options{})
	ctx, cancel := hub.Bind(t.Context())
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("the context ended before the hub closed")
	default:
	}
	hub.Close()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the context did not end when the hub closed")
	}
}

func eventually(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s never held", description)
}
