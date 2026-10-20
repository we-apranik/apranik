package doh_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-apranik/apranik/module/dns/doh"
	"github.com/we-apranik/apranik/spec"
)

type plainDialer struct {
	target string
	dials  atomic.Int64
}

func (d *plainDialer) DialStream(ctx context.Context, _ spec.Destination) (spec.Stream, error) {
	d.dials.Add(1)
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", d.target)
	if err != nil {
		return nil, err
	}
	return connection.(*net.TCPConn), nil
}

func (d *plainDialer) Close() error { return nil }

func newServer(t *testing.T, settings string, handler http.HandlerFunc) (spec.DNSServer, *plainDialer) {
	t.Helper()
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	module := doh.NewStreamDNSServerModule()
	if err := json.Unmarshal([]byte(settings), module); err != nil {
		t.Fatal(err)
	}
	if err := module.Validate(); err != nil {
		t.Fatal(err)
	}
	dialer := &plainDialer{target: strings.TrimPrefix(httpServer.URL, "http://")}
	server, err := module.NewStreamDNSServer(dialer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return server, dialer
}

var message = append([]byte{0x12, 0x34}, make([]byte, 30)...)

func answer(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	body[2] |= 0x80
	writer.Header().Set("Content-Type", "application/dns-message")
	writer.Write(body)
}

func TestQueryIsAnRFC8484PostWithoutAUserAgent(t *testing.T) {
	var seen *http.Request
	server, _ := newServer(t, `{"server": "8.8.8.8", "host": "dns.google"}`, func(writer http.ResponseWriter, request *http.Request) {
		seen = request.Clone(context.Background())
		answer(writer, request)
	})
	response, err := server.Query(context.Background(), message)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response[:2], message[:2]) || response[2]&0x80 == 0 {
		t.Fatalf("response %x", response)
	}
	if seen.Method != http.MethodPost || seen.URL.Path != doh.DefaultPath || seen.Host != "dns.google" {
		t.Errorf("request %s %s host %s", seen.Method, seen.URL.Path, seen.Host)
	}
	if seen.Header.Get("Content-Type") != "application/dns-message" || seen.Header.Get("Accept") != "application/dns-message" {
		t.Errorf("headers %v", seen.Header)
	}
	if userAgent, sent := seen.Header["User-Agent"]; sent {
		t.Errorf("a User-Agent was sent: %v", userAgent)
	}
}

func TestQueriesReuseTheConnection(t *testing.T) {
	server, dialer := newServer(t, `{"server": "dns.example", "path": "/q"}`, answer)
	for range 4 {
		if _, err := server.Query(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	if got := dialer.dials.Load(); got != 1 {
		t.Fatalf("%d dials for 4 queries, want 1", got)
	}
}

func TestAnswersThatAreNotDNSMessagesAreRefused(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"an error status": func(writer http.ResponseWriter, _ *http.Request) { http.Error(writer, "no", http.StatusBadGateway) },
		"another media type": func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "text/html")
			writer.Write([]byte("<html>blocked</html>"))
		},
		"a redirect": func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "http://evil.example/dns-query", http.StatusFound)
		},
		"an oversized body": func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/dns-message")
			writer.Write(make([]byte, 70000))
		},
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := newServer(t, `{"server": "8.8.8.8"}`, handler)
			if _, err := server.Query(context.Background(), message); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestModuleDeclaresWhatItNeeds(t *testing.T) {
	module := doh.NewStreamDNSServerModule()
	if !module.NeedsTLS() {
		t.Error("doh does not ask for TLS")
	}
	provider, provides := module.(spec.TransportInfoProvider)
	if !provides || len(provider.TransportInfo().ApplicationProtocols) != 1 || provider.TransportInfo().ApplicationProtocols[0] != "http/1.1" {
		t.Error("doh does not declare that it speaks only http/1.1")
	}
}

func TestSettingsAreValidated(t *testing.T) {
	for _, settings := range []string{
		`{}`,
		`{"server": "8.8.8.8", "timeout": "-1s"}`,
		`{"server": "8.8.8.8", "path": "dns-query"}`,
		`{"server": "8.8.8.8", "path": "//evil.example/x"}`,
		`{"server": "8.8.8.8", "host": "bad host"}`,
	} {
		module := doh.NewStreamDNSServerModule()
		json.Unmarshal([]byte(settings), module)
		if module.Validate() == nil {
			t.Errorf("%s accepted", settings)
		}
	}
}
