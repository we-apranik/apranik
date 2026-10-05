package outbound

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/spec"
)

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

func httpsProbeTargetTrusting(t *testing.T, server *httptest.Server) probeTarget {
	t.Helper()
	target, err := newProbeTarget(ProbeConfig{UseTLS: true, URL: server.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
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
	elapsed, err := target.measure(context.Background(), fixedAddressMember{address: server.Listener.Addr().String()}, 5*time.Second)
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
	if _, err := target.measure(context.Background(), fixedAddressMember{address: server.Listener.Addr().String()}, 5*time.Second); err == nil {
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

	target, err := newProbeTarget(ProbeConfig{UseTLS: true, URL: server.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.measure(context.Background(), fixedAddressMember{address: server.Listener.Addr().String()}, 5*time.Second); err == nil {
		t.Error("an untrusted certificate passed the probe")
	}
}

func TestAnHTTPSProbeFailsAgainstAPlainHTTPServer(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(plain.Close)

	target, err := newProbeTarget(ProbeConfig{UseTLS: true, URL: "https://127.0.0.1:1/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.measure(context.Background(), fixedAddressMember{address: plain.Listener.Addr().String()}, 5*time.Second); err == nil {
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

	target, err := newProbeTarget(ProbeConfig{URL: "http://127.0.0.1:1/"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := target.measure(context.Background(), fixedAddressMember{address: listener.Addr().String()}, 200*time.Millisecond); err == nil {
		t.Fatal("a silent target passed the probe")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("the probe took %s to give up on a 200ms timeout", elapsed)
	}
}

func TestProbeTargetDefaultsAndPorts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   ProbeConfig
		wantHost string
		wantPort uint16
	}{
		{"http default", ProbeConfig{}, "www.gstatic.com", 80},
		{"https default", ProbeConfig{UseTLS: true}, "www.gstatic.com", 443},
		{"explicit port", ProbeConfig{URL: "http://example.com:8080/x"}, "example.com", 8080},
		{"an IP", ProbeConfig{URL: "http://192.0.2.1/"}, "192.0.2.1", 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, err := newProbeTarget(tc.config)
			if err != nil {
				t.Fatal(err)
			}
			if target.destination.Host() != tc.wantHost || target.destination.Port() != tc.wantPort {
				t.Errorf("destination = %s, want %s:%d", target.destination, tc.wantHost, tc.wantPort)
			}
		})
	}
}
