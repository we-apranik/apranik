package testkit

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/we-apranik/apranik/module/api/clash/internal/stream"
	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

const Tick = 20 * time.Millisecond

type Env struct {
	T       *testing.T
	Hub     *stream.Hub
	BaseURL string
	server  *httptest.Server
}

func Mount(t *testing.T, register func(router *web.Router, hub *stream.Hub)) *Env {
	t.Helper()
	hub := stream.NewHub(stream.Options{Tick: Tick})
	router := web.NewRouter()
	register(router, hub)
	server := httptest.NewServer(router)
	t.Cleanup(func() {
		hub.Close()
		server.Close()
		hub.Wait()
	})
	return &Env{T: t, Hub: hub, BaseURL: server.URL, server: server}
}

func (e *Env) Do(method, path, body string, headers ...string) (int, string, http.Header) {
	e.T.Helper()
	request, err := http.NewRequest(method, e.BaseURL+path, strings.NewReader(body))
	if err != nil {
		e.T.Fatal(err)
	}
	for index := 0; index+1 < len(headers); index += 2 {
		request.Header.Set(headers[index], headers[index+1])
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		e.T.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(data), response.Header
}

func (e *Env) GetJSON(path string, into any) int {
	e.T.Helper()
	status, body, _ := e.Do("GET", path, "")
	if into != nil && status == http.StatusOK {
		if err := json.Unmarshal([]byte(body), into); err != nil {
			e.T.Fatalf("GET %s: %q is not the JSON expected: %v", path, body, err)
		}
	}
	return status
}

func MessageOf(t *testing.T, body string) string {
	t.Helper()
	var parsed struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || parsed.Message == "" {
		t.Fatalf("%q is not an error body with a message", body)
	}
	return parsed.Message
}

func (e *Env) OpenLines(path string) (*http.Response, *bufio.Reader) {
	e.T.Helper()
	response, err := http.Get(e.BaseURL + path)
	if err != nil {
		e.T.Fatal(err)
	}
	e.T.Cleanup(func() { response.Body.Close() })
	return response, bufio.NewReader(response.Body)
}

func ReadLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading a line of the stream: %v", err)
	}
	return strings.TrimSpace(line)
}

func (e *Env) DialWebSocket(path, origin string) (*websocket.Conn, error) {
	e.T.Helper()
	if origin == "" {
		origin = e.BaseURL
	}
	connection, err := websocket.Dial("ws"+strings.TrimPrefix(e.BaseURL, "http")+path, "", origin)
	if err == nil {
		e.T.Cleanup(func() { connection.Close() })
	}
	return connection, err
}

func (e *Env) UpgradeStatus(path string, extraHeaders ...string) int {
	e.T.Helper()
	request, err := http.NewRequest("GET", e.BaseURL+path, nil)
	if err != nil {
		e.T.Fatal(err)
	}
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	for index := 0; index+1 < len(extraHeaders); index += 2 {
		request.Header.Set(extraHeaders[index], extraHeaders[index+1])
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		e.T.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func ReadText(t *testing.T, connection *websocket.Conn) string {
	t.Helper()
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message string
	if err := websocket.Message.Receive(connection, &message); err != nil {
		t.Fatalf("expected a text message: %v", err)
	}
	return message
}

func Eventually(t *testing.T, description string, condition func() bool) {
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

var _ spec.APICoreProvider = (*Provider)(nil)
