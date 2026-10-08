package tls_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/transport/overlay/tls"
	"github.com/The-Apranik/Apranik/spec"
)

// testAuthority is a certificate authority made in memory, so no key material
// lives in the repository.
type testAuthority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         string
}

func newTestAuthority(t testing.TB) *testAuthority {
	t.Helper()
	key := newKey(t)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Apranik test authority"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &testAuthority{certificate: certificate, key: key, pem: encodePEM("CERTIFICATE", der)}
}

// issue returns a leaf certificate and key, both PEM, for the given names.
func (a *testAuthority) issue(t testing.TB, domains []string, ips []netip.Addr) (certificatePEM, keyPEM string) {
	t.Helper()
	key := newKey(t)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     domains,
	}
	for _, ip := range ips {
		template.IPAddresses = append(template.IPAddresses, ip.AsSlice())
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.certificate, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return encodePEM("CERTIFICATE", der), encodePEM("EC PRIVATE KEY", keyDER)
}

func newKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func encodePEM(blockType string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
}

// newModule is what the config would decode: defaults, then the test's edits.
func newModule(t testing.TB, edit func(*tls.Settings)) *tls.Transport {
	t.Helper()
	transport := tls.NewOverlayStreamTransportModule().(*tls.Transport)
	edit(&transport.Settings)
	if err := transport.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return transport
}

// tcpPair is a connected loopback TCP pair: real half-close, real deadlines.
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

func destination(t testing.TB, host string, port uint16) spec.Destination {
	t.Helper()
	if ip, err := netip.ParseAddr(host); err == nil {
		server, err := spec.NewDestinationFromIP(ip, port)
		if err != nil {
			t.Fatal(err)
		}
		return server
	}
	server, err := spec.NewDestinationFromDomain(host, port)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

type handshakeResult struct {
	stream spec.Stream
	err    error
}

// handshake runs both sides at once and returns each side's outcome.
func handshake(t *testing.T, clientModule, serverModule *tls.Transport, server spec.Destination) (clientResult, serverResult handshakeResult) {
	t.Helper()
	overlayClient, err := clientModule.NewStreamOverlayClient()
	if err != nil {
		t.Fatalf("NewStreamOverlayClient: %v", err)
	}
	overlayServer, err := serverModule.NewStreamOverlayServer()
	if err != nil {
		t.Fatalf("NewStreamOverlayServer: %v", err)
	}

	clientSide, serverSide := tcpPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverDone := make(chan handshakeResult, 1)
	go func() {
		stream, err := overlayServer.WrapServerStream(ctx, serverSide, &spec.TransportMetadata{})
		if err != nil {
			serverSide.Close() // as core does, so the client is not left waiting
		}
		serverDone <- handshakeResult{stream, err}
	}()
	stream, err := overlayClient.WrapClientStream(ctx, clientSide, server)
	if err != nil {
		clientSide.Close()
	}
	return handshakeResult{stream, err}, <-serverDone
}

func serverFor(t *testing.T, authority *testAuthority, domains []string, ips []netip.Addr) *tls.Transport {
	certificatePEM, keyPEM := authority.issue(t, domains, ips)
	return newModule(t, func(s *tls.Settings) {
		s.Certificates = []tls.Certificate{{Certificate: certificatePEM, Key: types.Secret(keyPEM)}}
	})
}

func clientTrusting(t testing.TB, authority *testAuthority, edit func(*tls.Settings)) *tls.Transport {
	return newModule(t, func(s *tls.Settings) {
		s.CertificateAuthorities = []tls.CertificateAuthority{{Certificate: authority.pem}}
		if edit != nil {
			edit(s)
		}
	})
}

func TestBytesCrossBothWays(t *testing.T) {
	authority := newTestAuthority(t)
	clientResult, serverResult := handshake(t,
		clientTrusting(t, authority, nil),
		serverFor(t, authority, []string{"example.com"}, nil),
		destination(t, "example.com", 443))
	if clientResult.err != nil || serverResult.err != nil {
		t.Fatalf("handshake: client %v, server %v", clientResult.err, serverResult.err)
	}

	go clientResult.stream.Write([]byte("ping"))
	received := make([]byte, 4)
	if _, err := io.ReadFull(serverResult.stream, received); err != nil || string(received) != "ping" {
		t.Fatalf("server read %q, %v", received, err)
	}
	go serverResult.stream.Write([]byte("pong"))
	if _, err := io.ReadFull(clientResult.stream, received); err != nil || string(received) != "pong" {
		t.Fatalf("client read %q, %v", received, err)
	}
}

func TestTheServersDomainIsTheDefaultServerName(t *testing.T) {
	authority := newTestAuthority(t)
	_, serverResult := handshake(t,
		clientTrusting(t, authority, nil),
		serverFor(t, authority, []string{"example.com"}, nil),
		destination(t, "example.com", 443))
	if serverResult.err != nil {
		t.Fatalf("server: %v", serverResult.err)
	}
	if got := serverResult.stream.(interface {
		ConnectionState() cryptotls.ConnectionState
	}).ConnectionState().ServerName; got != "example.com" {
		t.Errorf("server saw SNI %q, want example.com", got)
	}
}

func TestServerNameOverridesTheServersAddress(t *testing.T) {
	authority := newTestAuthority(t)
	clientResult, _ := handshake(t,
		clientTrusting(t, authority, func(s *tls.Settings) { s.ServerName = "front.example" }),
		serverFor(t, authority, []string{"front.example"}, nil),
		destination(t, "203.0.113.7", 443))
	if clientResult.err != nil {
		t.Fatalf("client: %v", clientResult.err)
	}
}

func TestAServerGivenAsAnIPIsCheckedAgainstTheCertificatesIPs(t *testing.T) {
	authority := newTestAuthority(t)
	loopback := netip.MustParseAddr("127.0.0.1")
	clientResult, serverResult := handshake(t,
		clientTrusting(t, authority, nil),
		serverFor(t, authority, nil, []netip.Addr{loopback}),
		destination(t, "127.0.0.1", 443))
	if clientResult.err != nil {
		t.Fatalf("client: %v", clientResult.err)
	}
	if sni := serverResult.stream.(interface {
		ConnectionState() cryptotls.ConnectionState
	}).ConnectionState().ServerName; sni != "" {
		t.Errorf("SNI %q was sent for an IP", sni)
	}
}

func TestAWrongNameOrUnknownAuthorityFailsTheClient(t *testing.T) {
	authority := newTestAuthority(t)
	for name, clientModule := range map[string]*tls.Transport{
		"wrong name":        clientTrusting(t, authority, func(s *tls.Settings) { s.ServerName = "other.example" }),
		"unknown authority": clientTrusting(t, newTestAuthority(t), nil),
	} {
		clientResult, _ := handshake(t, clientModule, serverFor(t, authority, []string{"example.com"}, nil), destination(t, "example.com", 443))
		if clientResult.err == nil {
			t.Errorf("%s: handshake succeeded", name)
		} else if !strings.Contains(clientResult.err.Error(), "example.com:443") {
			t.Errorf("%s: error %q does not name the server", name, clientResult.err)
		}
	}
}

func TestInsecureAcceptsAnyCertificate(t *testing.T) {
	authority := newTestAuthority(t)
	clientResult, _ := handshake(t,
		newModule(t, func(s *tls.Settings) { s.Insecure = true }),
		serverFor(t, authority, []string{"example.com"}, nil),
		destination(t, "other.example", 443))
	if clientResult.err != nil {
		t.Fatalf("client: %v", clientResult.err)
	}
}

func TestALPNIsNegotiated(t *testing.T) {
	authority := newTestAuthority(t)
	certificatePEM, keyPEM := authority.issue(t, []string{"example.com"}, nil)
	serverModule := newModule(t, func(s *tls.Settings) {
		s.Certificates = []tls.Certificate{{Certificate: certificatePEM, Key: types.Secret(keyPEM)}}
		s.ALPN = []string{"h2", "http/1.1"}
	})
	clientResult, serverResult := handshake(t,
		clientTrusting(t, authority, func(s *tls.Settings) { s.ALPN = []string{"http/1.1"} }),
		serverModule, destination(t, "example.com", 443))
	if clientResult.err != nil {
		t.Fatalf("client: %v", clientResult.err)
	}
	if got := serverResult.stream.(interface {
		ConnectionState() cryptotls.ConnectionState
	}).ConnectionState().NegotiatedProtocol; got != "http/1.1" {
		t.Errorf("negotiated %q, want http/1.1", got)
	}
}

func TestVersionsOutsideTheRangeAreRefused(t *testing.T) {
	authority := newTestAuthority(t)
	certificatePEM, keyPEM := authority.issue(t, []string{"example.com"}, nil)
	serverModule := newModule(t, func(s *tls.Settings) {
		s.Certificates = []tls.Certificate{{Certificate: certificatePEM, Key: types.Secret(keyPEM)}}
		s.MinVersion = "1.3"
	})
	clientResult, _ := handshake(t,
		clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint, s.MaxVersion = "none", "1.2" }),
		serverModule, destination(t, "example.com", 443))
	if clientResult.err == nil {
		t.Fatal("a 1.2-only client reached a 1.3-only server")
	}
}

// Half-closing ends one direction only: the peer reads the end, then can
// still answer.
func TestCloseWriteEndsOneDirection(t *testing.T) {
	authority := newTestAuthority(t)
	clientResult, serverResult := handshake(t,
		clientTrusting(t, authority, nil),
		serverFor(t, authority, []string{"example.com"}, nil),
		destination(t, "example.com", 443))
	if clientResult.err != nil || serverResult.err != nil {
		t.Fatalf("handshake: client %v, server %v", clientResult.err, serverResult.err)
	}

	clientResult.stream.Write([]byte("last words"))
	if err := clientResult.stream.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}
	everything, err := io.ReadAll(serverResult.stream)
	if err != nil || string(everything) != "last words" {
		t.Fatalf("server read %q, %v; want the payload then end of stream", everything, err)
	}

	go serverResult.stream.Write([]byte("reply"))
	reply := make([]byte, 5)
	if _, err := io.ReadFull(clientResult.stream, reply); err != nil || string(reply) != "reply" {
		t.Fatalf("client read %q, %v after its own CloseWrite", reply, err)
	}
}

// Core bounds the server's handshake with its context; a client that sends
// nothing must not hold it past that.
func TestACancelledContextEndsAHandshakeWithASilentClient(t *testing.T) {
	authority := newTestAuthority(t)
	overlayServer, err := serverFor(t, authority, []string{"example.com"}, nil).NewStreamOverlayServer()
	if err != nil {
		t.Fatal(err)
	}
	_, serverSide := tcpPair(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := overlayServer.WrapServerStream(ctx, serverSide, &spec.TransportMetadata{})
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

func TestAServerIsRefusedAtBuildWithoutAUsableCertificate(t *testing.T) {
	authority := newTestAuthority(t)
	certificatePEM, keyPEM := authority.issue(t, []string{"example.com"}, nil)
	_, otherKeyPEM := authority.issue(t, []string{"example.com"}, nil)
	missing := filepath.Join(t.TempDir(), "missing.pem")

	for name, certificates := range map[string][]tls.Certificate{
		"none":         nil,
		"missing file": {{CertificateFile: missing, Key: types.Secret(keyPEM)}},
		"wrong key":    {{Certificate: certificatePEM, Key: types.Secret(otherKeyPEM)}},
	} {
		module := newModule(t, func(s *tls.Settings) { s.Certificates = certificates })
		_, err := module.NewStreamOverlayServer()
		if err == nil {
			t.Errorf("%s: a server was built", name)
			continue
		}
		if strings.Contains(err.Error(), "PRIVATE KEY") {
			t.Errorf("%s: the error carries key material: %v", name, err)
		}
	}
}

func TestCertificatesAreReadFromFiles(t *testing.T) {
	authority := newTestAuthority(t)
	certificatePEM, keyPEM := authority.issue(t, []string{"example.com"}, nil)
	directory := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	serverModule := newModule(t, func(s *tls.Settings) {
		s.Certificates = []tls.Certificate{{CertificateFile: write("cert.pem", certificatePEM), KeyFile: write("key.pem", keyPEM)}}
	})
	clientModule := newModule(t, func(s *tls.Settings) {
		s.CertificateAuthorities = []tls.CertificateAuthority{{CertificateFile: write("ca.pem", authority.pem)}}
	})
	if clientResult, _ := handshake(t, clientModule, serverModule, destination(t, "example.com", 443)); clientResult.err != nil {
		t.Fatalf("client: %v", clientResult.err)
	}
}

func TestValidateRefusesMalformedSettings(t *testing.T) {
	for name, edit := range map[string]func(*tls.Settings){
		"unknown version":        func(s *tls.Settings) { s.MinVersion = "1.4" },
		"min above max":          func(s *tls.Settings) { s.MinVersion, s.MaxVersion = "1.3", "1.2" },
		"empty alpn":             func(s *tls.Settings) { s.ALPN = []string{""} },
		"authority with neither": func(s *tls.Settings) { s.CertificateAuthorities = []tls.CertificateAuthority{{}} },
		"authority with both": func(s *tls.Settings) {
			s.CertificateAuthorities = []tls.CertificateAuthority{{CertificateFile: "a", Certificate: "b"}}
		},
		"certificate without key":        func(s *tls.Settings) { s.Certificates = []tls.Certificate{{CertificateFile: "a"}} },
		"unknown fingerprint":            func(s *tls.Settings) { s.Fingerprint = "netscape" },
		"fingerprint with a min version": func(s *tls.Settings) { s.Fingerprint, s.MinVersion = "firefox", "1.3" },
		"fingerprint with a max version": func(s *tls.Settings) { s.Fingerprint, s.MaxVersion = "chrome", "1.2" },
		"key twice": func(s *tls.Settings) {
			s.Certificates = []tls.Certificate{{CertificateFile: "a", KeyFile: "b", Key: "secret-key-material"}}
		},
	} {
		transport := tls.NewOverlayStreamTransportModule().(*tls.Transport)
		edit(&transport.Settings)
		err := transport.Validate()
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "secret-key-material") {
			t.Errorf("%s: the error carries the key: %v", name, err)
		}
	}
}

func TestWarnings(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*tls.Settings)
		want int
	}{
		"defaults": {func(*tls.Settings) {}, 0},
		"insecure": {func(s *tls.Settings) { s.Insecure = true }, 1},
		"insecure with a ca": {func(s *tls.Settings) {
			s.Insecure = true
			s.CertificateAuthorities = []tls.CertificateAuthority{{Certificate: "x"}}
		}, 2},
		"old version":       {func(s *tls.Settings) { s.MinVersion = "1.0" }, 1},
		"stale fingerprint": {func(s *tls.Settings) { s.Fingerprint = "edge" }, 1},
		"older version":     {func(s *tls.Settings) { s.Fingerprint = "hellochrome_120" }, 1},
		"current version":   {func(s *tls.Settings) { s.Fingerprint = "hellochrome_133" }, 0},
	} {
		transport := tls.NewOverlayStreamTransportModule().(*tls.Transport)
		tc.edit(&transport.Settings)
		if warnings := transport.Warnings(); len(warnings) != tc.want {
			t.Errorf("%s: %d warnings %v, want %d", name, len(warnings), warnings, tc.want)
		}
	}
}

func TestInfoIsTheSameForEveryValue(t *testing.T) {
	first, second := tls.NewOverlayStreamTransportModule(), tls.NewOverlayStreamTransportModule()
	if first.Info() != second.Info() || first.Info().Name != "tls" {
		t.Errorf("Info %v and %v", first.Info(), second.Info())
	}
}

func TestAServerGivesUpOnASilentClientAtItsHandshakeTimeout(t *testing.T) {
	serverModule := serverFor(t, newTestAuthority(t), []string{"proxy.example"}, nil)
	serverModule.HandshakeTimeout = types.Pointer(types.Duration(100 * time.Millisecond))
	overlayServer, err := serverModule.NewStreamOverlayServer()
	if err != nil {
		t.Fatal(err)
	}
	_, serverSide := tcpPair(t)
	result := make(chan error, 1)
	go func() {
		_, err := overlayServer.WrapServerStream(context.Background(), serverSide, &spec.TransportMetadata{})
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Error("a silent client completed a handshake")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a silent client held the handshake past handshake_timeout")
	}
}
