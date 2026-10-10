package clash

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/spec"
)

const testSecret = "s3cret"

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// A running server, closed when the test ends.
type running struct {
	t       *testing.T
	server  *server
	address string
}

func start(t *testing.T, provider spec.APICoreProvider, configure ...func(*Settings)) *running {
	t.Helper()
	settings := Settings{Listen: "127.0.0.1", Port: freePort(t), Secret: testSecret}
	for _, change := range configure {
		change(&settings)
	}
	server := newServer(settings, 20*time.Millisecond)
	if err := server.Start(t.Context(), provider); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return &running{t: t, server: server, address: "127.0.0.1:" + strconv.Itoa(settings.Port)}
}

// request sends a request, with the secret unless headers say otherwise.
func (r *running) request(method, path, body string, headers ...string) (int, string, http.Header) {
	r.t.Helper()
	request, err := http.NewRequest(method, "http://"+r.address+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testSecret)
	for index := 0; index+1 < len(headers); index += 2 {
		if headers[index+1] == "" {
			request.Header.Del(headers[index])
		} else {
			request.Header.Set(headers[index], headers[index+1])
		}
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		r.t.Fatal(err)
	}
	defer response.Body.Close()
	var data strings.Builder
	buffer := bufio.NewReader(response.Body)
	for {
		chunk, err := buffer.ReadString('\n')
		data.WriteString(chunk)
		if err != nil {
			break
		}
	}
	return response.StatusCode, data.String(), response.Header
}

// dialWebSocket opens a WebSocket as a page of the given origin would; an empty
// origin is the server's own.
func (r *running) dialWebSocket(path, origin string) (*websocket.Conn, error) {
	r.t.Helper()
	if origin == "" {
		origin = "http://" + r.address
	}
	connection, err := websocket.Dial("ws://"+r.address+path, "", origin)
	if err == nil {
		r.t.Cleanup(func() { connection.Close() })
	}
	return connection, err
}

// upgradeStatus sends a WebSocket handshake and returns the status of the answer.
func (r *running) upgradeStatus(path string, headers ...string) int {
	r.t.Helper()
	request, _ := http.NewRequest("GET", "http://"+r.address+path, nil)
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	for index := 0; index+1 < len(headers); index += 2 {
		request.Header.Set(headers[index], headers[index+1])
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		r.t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func TestEveryEndpointNeedsTheSecret(t *testing.T) {
	server := start(t, &testkit.Provider{})
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/"}, {"GET", "/version"}, {"GET", "/configs"}, {"GET", "/traffic"}, {"GET", "/memory"},
		{"GET", "/connections"}, {"DELETE", "/connections"}, {"DELETE", "/connections/1"},
		{"GET", "/proxies"}, {"GET", "/proxies/a"}, {"PUT", "/proxies/a"}, {"GET", "/proxies/a/delay"},
		{"GET", "/group/a/delay"}, {"GET", "/logs"}, {"GET", "/logs/file"}, {"GET", "/rules"},
		{"GET", "/dns/query"}, {"POST", "/cache/dns/flush"}, {"POST", "/reload"}, {"GET", "/nothing"},
		{"GET", "/inbounds/a/users"}, {"POST", "/inbounds/a/users"}, {"GET", "/inbounds/a/users/b"}, {"DELETE", "/inbounds/a/users/b"},
		{"POST", "/inbounds/a/users:batchCreate"}, {"POST", "/inbounds/a/users:batchDelete"},
	} {
		for name, headers := range map[string][]string{
			"without the secret":  {"Authorization", ""},
			"with the wrong one":  {"Authorization", "Bearer nope"},
			"in the query string": nil,
		} {
			path := endpoint.path
			if headers == nil {
				headers = []string{"Authorization", ""}
				path += "?token=" + testSecret
			}
			if status, _, _ := server.request(endpoint.method, path, "", headers...); status != http.StatusUnauthorized {
				t.Errorf("%s %s %s: status %d, want 401", endpoint.method, endpoint.path, name, status)
			}
		}
	}
}

func TestTheSecretReachesEveryFamilyOfEndpoints(t *testing.T) {
	provider := &testkit.Provider{
		Version: "1.2.3", Tags: []string{"direct"},
		States:           map[string]spec.APIOutboundState{"direct": {Tag: "direct", Kind: spec.APIOutboundKindFreedom}},
		RoutingRulesFunc: func() ([]spec.APIRoutingRule, error) { return []spec.APIRoutingRule{{Location: "default"}}, nil },
		QueryDNSFunc: func(context.Context, string, string) (spec.APIDNSAnswer, error) {
			return spec.APIDNSAnswer{}, nil
		},
		MeasureFunc: func(context.Context, string, string, time.Duration, string) (time.Duration, error) {
			return time.Millisecond, nil
		},
		ListUsersFunc: func(string) ([]string, error) { return []string{"alice"}, nil },
	}
	server := start(t, provider)
	for _, endpoint := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/", 200}, {"GET", "/version", 200}, {"GET", "/configs", 200}, {"GET", "/connections", 200},
		{"GET", "/proxies", 200}, {"GET", "/proxies/direct/delay", 200}, {"GET", "/rules", 200},
		{"GET", "/dns/query?name=example.com", 200}, {"POST", "/cache/dns/flush", 204}, {"POST", "/reload", 202},
		{"DELETE", "/connections/1", 204}, {"GET", "/logs/file", 404},
		{"GET", "/inbounds/socks-in/users", 200}, {"GET", "/inbounds/socks-in/users/alice", 200},
	} {
		if status, body, _ := server.request(endpoint.method, endpoint.path, ""); status != endpoint.want {
			t.Errorf("%s %s = %d %q, want %d", endpoint.method, endpoint.path, status, body, endpoint.want)
		}
	}
	if status, body, _ := server.request("GET", "/version", ""); !strings.Contains(body, "Apranik 1.2.3") {
		t.Errorf("/version = %d %q", status, body)
	}
}

func TestEndpointsNotBuiltAre404Json(t *testing.T) {
	server := start(t, &testkit.Provider{})
	for _, path := range []string{"/providers/proxies", "/providers/rules", "/restart", "/script", "/profile"} {
		status, body, _ := server.request("GET", path, "")
		if status != http.StatusNotFound || !strings.Contains(body, `"message"`) {
			t.Errorf("%s = %d %q, want a JSON 404", path, status, body)
		}
	}
}

func TestWebSocketsNeedTheSecretInTheQueryStringAndTheStreamsWork(t *testing.T) {
	provider := &testkit.Provider{TrafficFunc: func() (spec.APITrafficTotal, error) { return spec.APITrafficTotal{UplinkBytes: 7}, nil }}
	server := start(t, provider)

	if status := server.upgradeStatus("/traffic"); status != http.StatusUnauthorized {
		t.Errorf("a WebSocket without the token = %d, want 401", status)
	}
	if status := server.upgradeStatus("/traffic?token=wrong"); status != http.StatusUnauthorized {
		t.Errorf("a WebSocket with the wrong token = %d, want 401", status)
	}
	connection, err := server.dialWebSocket("/traffic?token="+testSecret, "")
	if err != nil {
		t.Fatalf("with the token: %v", err)
	}
	if message := testkit.ReadText(t, connection); !strings.Contains(message, `"upTotal":7`) {
		t.Errorf("message %q", message)
	}
}

func TestCORSAndTheWebSocketOriginFollowAllowOrigins(t *testing.T) {
	provider := &testkit.Provider{Version: "1"}
	server := start(t, provider, func(s *Settings) { s.AllowOrigins = []string{"https://dash.example"} })

	_, _, header := server.request("GET", "/version", "", "Origin", "https://dash.example")
	if header.Get("Access-Control-Allow-Origin") != "https://dash.example" {
		t.Errorf("headers = %v", header)
	}
	status, _, header := server.request("OPTIONS", "/proxies", "", "Authorization", "", "Origin", "https://dash.example", "Access-Control-Request-Method", "PUT")
	if status != http.StatusNoContent || header.Get("Access-Control-Allow-Origin") != "https://dash.example" {
		t.Errorf("a preflight without the secret = %d %v", status, header)
	}

	if _, err := server.dialWebSocket("/memory?token="+testSecret, "https://dash.example"); err != nil {
		t.Errorf("a WebSocket from the allowed origin: %v", err)
	}
	if status := server.upgradeStatus("/memory?token="+testSecret, "Origin", "https://evil.example"); status != http.StatusForbidden {
		t.Errorf("a WebSocket from another origin = %d, want 403", status)
	}
}

// What the handlers asked the provider for, safe to read from the test.
type asked struct {
	mu                     sync.Mutex
	buffer                 int
	rawURL, expectedStatus string
	timeout                time.Duration
}

func (a *asked) set(update func(*asked)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	update(a)
}

func (a *asked) get() asked {
	a.mu.Lock()
	defer a.mu.Unlock()
	return asked{buffer: a.buffer, rawURL: a.rawURL, expectedStatus: a.expectedStatus, timeout: a.timeout}
}

func TestLogBufferAndDelayDefaultsFromTheSettingsReachTheHandlers(t *testing.T) {
	subscription := testkit.NewLogSubscription()
	var seen asked
	provider := &testkit.Provider{
		Tags:   []string{"direct"},
		States: map[string]spec.APIOutboundState{"direct": {Tag: "direct", Kind: spec.APIOutboundKindFreedom}},
		SubscribeFunc: func(_ slog.Level, buffered int) (spec.APILogSubscription, error) {
			seen.set(func(a *asked) { a.buffer = buffered })
			return subscription, nil
		},
		MeasureFunc: func(_ context.Context, _, rawURL string, timeout time.Duration, expected string) (time.Duration, error) {
			seen.set(func(a *asked) { a.rawURL, a.timeout, a.expectedStatus = rawURL, timeout, expected })
			return time.Millisecond, nil
		},
	}
	configured := func(s *Settings) {
		buffer := 64
		timeout := types.Duration(3 * time.Second)
		s.LogBufferRecords = &buffer
		s.DelayTest = &DelayTestSettings{URL: "http://configured.example/204", Timeout: &timeout}
	}
	server := start(t, provider, configured)

	go server.request("GET", "/logs", "")
	testkit.Eventually(t, "the subscription", func() bool { return seen.get().buffer != 0 })
	if got := seen.get().buffer; got != 64 {
		t.Errorf("subscribed with a buffer of %d, want 64", got)
	}

	server.request("GET", "/proxies/direct/delay", "")
	if got := seen.get(); got.rawURL != "http://configured.example/204" || got.timeout != 3*time.Second || got.expectedStatus != "200-299" {
		t.Errorf("measured %q %v %q, want the settings and the module's own expected status", got.rawURL, got.timeout, got.expectedStatus)
	}

	bare := start(t, provider)
	bare.request("GET", "/proxies/direct/delay", "")
	if got := seen.get(); got.rawURL != "https://www.gstatic.com/generate_204" || got.timeout != 10*time.Second || got.expectedStatus != "200-299" {
		t.Errorf("measured %q %v %q, want the module's own defaults", got.rawURL, got.timeout, got.expectedStatus)
	}
	bare.request("GET", "/proxies/direct/delay?url=http://q.example/&timeout=250&expected=204", "")
	if got := seen.get(); got.rawURL != "http://q.example/" || got.timeout != 250*time.Millisecond || got.expectedStatus != "204" {
		t.Errorf("measured %q %v %q, want the query's values", got.rawURL, got.timeout, got.expectedStatus)
	}
}

func TestClosingTheServerEndsEveryStreamAndWaits(t *testing.T) {
	server := start(t, &testkit.Provider{})
	request, _ := http.NewRequest("GET", "http://"+server.address+"/memory", nil)
	request.Header.Set("Authorization", "Bearer "+testSecret)
	plain, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Body.Close()
	reader := bufio.NewReader(plain.Body)

	connection, err := server.dialWebSocket("/memory?token="+testSecret, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}

	closed := make(chan error, 1)
	go func() { closed <- server.server.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return while streams were open")
	}
	for {
		if _, err := reader.ReadString('\n'); err != nil {
			break
		}
	}
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var ignored string
		if err := websocket.Message.Receive(connection, &ignored); err != nil {
			break
		}
	}
	if _, err := net.DialTimeout("tcp", server.address, time.Second); err == nil {
		t.Error("the port still accepts connections after Close")
	}
}

func TestAServerCannotBeStartedTwiceOrAfterItWasClosedAndCloseIsRepeatable(t *testing.T) {
	server := start(t, &testkit.Provider{})
	if err := server.server.Start(t.Context(), &testkit.Provider{}); err == nil {
		t.Error("a started server started again")
	}
	if err := server.server.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
	if err := server.server.Close(); err != nil {
		t.Errorf("a second Close = %v", err)
	}
	if err := server.server.Start(t.Context(), &testkit.Provider{}); err == nil {
		t.Error("a closed server started")
	}
}

func TestCloseBeforeStartIsFine(t *testing.T) {
	if err := newServer(Settings{Listen: "127.0.0.1", Port: freePort(t), Secret: testSecret}, time.Second).Close(); err != nil {
		t.Errorf("Close before Start = %v", err)
	}
}

func TestStartNeedsAProviderAndAFreePort(t *testing.T) {
	settings := Settings{Listen: "127.0.0.1", Port: freePort(t), Secret: testSecret}
	if err := newServer(settings, time.Second).Start(t.Context(), nil); err == nil {
		t.Error("a server started without a provider")
	}

	first := start(t, &testkit.Provider{})
	port, _ := strconv.Atoi(first.address[strings.LastIndex(first.address, ":")+1:])
	taken := newServer(Settings{Listen: "127.0.0.1", Port: port, Secret: testSecret}, time.Second)
	if err := taken.Start(t.Context(), &testkit.Provider{}); err == nil {
		t.Error("Start on a taken port succeeded")
	}
}

func TestTheServerClosesWhenItsContextEnds(t *testing.T) {
	settings := Settings{Listen: "127.0.0.1", Port: freePort(t), Secret: testSecret}
	server := newServer(settings, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx, &testkit.Provider{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	testkit.Eventually(t, "the port freed", func() bool {
		listener, err := net.Listen("tcp", net.JoinHostPort(settings.Listen, strconv.Itoa(settings.Port)))
		if err != nil {
			return false
		}
		listener.Close()
		return true
	})
}

// The reload asks the core to close this very server. The answer is already
// written when the handler returns, so Close must let it reach the client.
func TestAReloadThatClosesTheServerStillDeliversItsAnswer(t *testing.T) {
	var server *running
	closed := make(chan struct{})
	provider := &testkit.Provider{RequestReloadFunc: func() error {
		go func() {
			server.server.Close()
			close(closed)
		}()
		time.Sleep(50 * time.Millisecond)
		return nil
	}}
	server = start(t, provider)

	status, body, _ := server.request("POST", "/reload", "")
	if status != http.StatusAccepted || !strings.Contains(body, "being loaded") {
		t.Errorf("answer = %d %q", status, body)
	}
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the server never finished closing")
	}
}

func TestTheDashboardIsServedWithoutTheSecretAndTheAPIStillNeedsIt(t *testing.T) {
	directory := t.TempDir()
	os.WriteFile(filepath.Join(directory, "index.html"), []byte("<h1>dashboard</h1>"), 0o644)
	server := start(t, &testkit.Provider{}, func(s *Settings) { s.ExternalUI = directory })

	if status, body, _ := server.request("GET", "/ui/", "", "Authorization", ""); status != http.StatusOK || !strings.Contains(body, "dashboard") {
		t.Errorf("/ui/ without the secret = %d %q", status, body)
	}
	if status, _, _ := server.request("GET", "/version", "", "Authorization", ""); status != http.StatusUnauthorized {
		t.Errorf("/version without the secret = %d, want 401", status)
	}
}

func TestWithoutExternalUIThereIsNoDashboard(t *testing.T) {
	server := start(t, &testkit.Provider{})
	if status, _, _ := server.request("GET", "/ui/", "", "Authorization", ""); status != http.StatusUnauthorized {
		t.Errorf("/ui/ = %d, want the API's 401 like any other path", status)
	}
}

func TestStartFailsForADashboardFolderThatDoesNotExist(t *testing.T) {
	settings := Settings{Listen: "127.0.0.1", Port: freePort(t), Secret: testSecret, ExternalUI: filepath.Join(t.TempDir(), "nope")}
	if err := newServer(settings, time.Second).Start(t.Context(), &testkit.Provider{}); err == nil || !strings.Contains(err.Error(), "external_ui") {
		t.Errorf("Start = %v", err)
	}
}
