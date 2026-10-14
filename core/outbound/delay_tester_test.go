package outbound

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/shared/httpstatusranges"
	"github.com/we-apranik/apranik/spec"
)

var any2xx = []httpstatusranges.Range{{First: 200, Last: 299}}

func newTester(t *testing.T, rawURL string) DelayTester {
	t.Helper()
	tester, err := NewDelayTester(rawURL, any2xx)
	if err != nil {
		t.Fatal(err)
	}
	return tester
}

// A member that reaches one address whatever it is asked for.
type fixedAddressMember struct{ address string }

func (fixedAddressMember) Tag() string              { return "member" }
func (fixedAddressMember) Networks() []spec.Network { return []spec.Network{spec.NetworkTCP} }
func (fixedAddressMember) Close() error             { return nil }
func (fixedAddressMember) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return nil, spec.ErrUnsupportedNetwork
}
func (m fixedAddressMember) DialStream(ctx context.Context, _ spec.Metadata) (spec.Stream, error) {
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", m.address)
	if err != nil {
		return nil, err
	}
	return connection.(*net.TCPConn), nil
}

var _ component.Outbound = fixedAddressMember{}

func httpsProbeTargetTrusting(t *testing.T, server *httptest.Server) DelayTester {
	t.Helper()
	target := newTester(t, server.URL+"/")
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	target.rootCAs = roots
	return target
}

func TestAnHTTPSProbePassesThroughTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	target := httpsProbeTargetTrusting(t, server)
	elapsed, err := target.MeasureDelay(context.Background(), fixedAddressMember{address: server.Listener.Addr().String()}, 5*time.Second)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if elapsed <= 0 {
		t.Errorf("elapsed = %s, want a positive time", elapsed)
	}
}

func TestAnHTTPSProbeFailsOnANon2xxStatus(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(server.Close)

	target := httpsProbeTargetTrusting(t, server)
	if _, err := target.MeasureDelay(context.Background(), fixedAddressMember{address: server.Listener.Addr().String()}, 5*time.Second); err == nil {
		t.Error("a redirect passed the probe")
	}
}

// The system's roots do not know a test server's certificate, which is what a
// probe to an impostor would meet.
func TestAnHTTPSProbeRefusesAnUntrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	target := newTester(t, server.URL+"/")
	if _, err := target.MeasureDelay(context.Background(), fixedAddressMember{address: server.Listener.Addr().String()}, 5*time.Second); err == nil {
		t.Error("an untrusted certificate passed the probe")
	}
}

func TestAnHTTPSProbeFailsAgainstAPlainHTTPServer(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(plain.Close)

	target := newTester(t, "https://127.0.0.1:1/")
	if _, err := target.MeasureDelay(context.Background(), fixedAddressMember{address: plain.Listener.Addr().String()}, 5*time.Second); err == nil {
		t.Error("a TLS probe passed against a server that does not speak TLS")
	}
}

func TestAProbeTimesOutOnASilentTarget(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { connection.Close() })
		}
	}()

	target := newTester(t, "http://127.0.0.1:1/")
	start := time.Now()
	if _, err := target.MeasureDelay(context.Background(), fixedAddressMember{address: listener.Addr().String()}, 200*time.Millisecond); err == nil {
		t.Fatal("a silent target passed the probe")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("the probe took %s to give up on a 200ms timeout", elapsed)
	}
}

func TestDelayTesterReadsTheDestinationFromTheURL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		url      string
		wantHost string
		wantPort uint16
		wantTLS  bool
	}{
		{"http", "http://example.com/x", "example.com", 80, false},
		{"https", "https://example.com/x", "example.com", 443, true},
		{"explicit port", "http://example.com:8080/x", "example.com", 8080, false},
		{"an IP", "http://192.0.2.1/", "192.0.2.1", 80, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := newTester(t, tc.url)
			if target.destination.Host() != tc.wantHost || target.destination.Port() != tc.wantPort || target.useTLS != tc.wantTLS {
				t.Errorf("destination = %s, tls = %v; want %s:%d, tls = %v", target.destination, target.useTLS, tc.wantHost, tc.wantPort, tc.wantTLS)
			}
		})
	}
}

func TestDelayTesterRefusesWhatItCannotMeasureWith(t *testing.T) {
	for name, tc := range map[string]struct {
		url      string
		statuses []httpstatusranges.Range
	}{
		"no url":         {"", any2xx},
		"ftp":            {"ftp://example.com/", any2xx},
		"a bare word":    {"example.com", any2xx},
		"no host":        {"http:///path", any2xx},
		"port zero":      {"http://example.com:0/", any2xx},
		"no statuses":    {"http://example.com/", nil},
		"too big a port": {"http://example.com:99999/", any2xx},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDelayTester(tc.url, tc.statuses); err == nil {
				t.Errorf("NewDelayTester(%q, %v) succeeded", tc.url, tc.statuses)
			}
		})
	}
}

func TestMeasureDelayNeedsATimeout(t *testing.T) {
	target := newTester(t, "http://127.0.0.1:1/")
	if _, err := target.MeasureDelay(context.Background(), fixedAddressMember{address: "127.0.0.1:1"}, 0); err == nil {
		t.Error("a zero timeout was accepted")
	}
}

func answeringWith(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(status) }))
}

func TestOnlyTheExpectedStatusesPass(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		expected string
		passes   bool
	}{
		{"a 204 passes the 2xx class", http.StatusNoContent, "200-299", true},
		{"a 500 fails the 2xx class", http.StatusInternalServerError, "200-299", false},
		{"a 204 fails when only 200 is expected", http.StatusNoContent, "200", false},
		{"a 302 passes when listed", http.StatusFound, "200,302", true},
		{"anything passes 100-599", http.StatusInternalServerError, "100-599", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := answeringWith(tc.status)
			t.Cleanup(server.Close)
			expected, err := httpstatusranges.Parse(tc.expected)
			if err != nil {
				t.Fatal(err)
			}
			target, err := NewDelayTester(server.URL+"/", expected)
			if err != nil {
				t.Fatal(err)
			}
			_, err = target.MeasureDelay(context.Background(), fixedAddressMember{address: server.Listener.Addr().String()}, 5*time.Second)
			if (err == nil) != tc.passes {
				t.Fatalf("status %d against %q: err = %v, want pass = %v", tc.status, tc.expected, err, tc.passes)
			}
			if err != nil && !strings.Contains(err.Error(), "expected "+tc.expected) {
				t.Errorf("the error does not name what was expected: %v", err)
			}
		})
	}
}

func TestEveryKindOfTimeoutWrapsDeadlineExceeded(t *testing.T) {
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { silent.Close() })
	go func() {
		for {
			connection, err := silent.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { connection.Close() })
		}
	}()

	target := newTester(t, "http://127.0.0.1:1/")
	_, err = target.MeasureDelay(context.Background(), fixedAddressMember{address: silent.Addr().String()}, 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a read timeout = %v, want an error wrapping context.DeadlineExceeded", err)
	}

	_, err = target.MeasureDelay(context.Background(), blockedDialMember{}, 100*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a dial that waits for the context = %v, want an error wrapping context.DeadlineExceeded", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = target.MeasureDelay(cancelled, blockedDialMember{}, time.Second); errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a cancelled caller was reported as a timeout: %v", err)
	}
}

type blockedDialMember struct{ fixedAddressMember }

func (blockedDialMember) DialStream(ctx context.Context, _ spec.Metadata) (spec.Stream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
