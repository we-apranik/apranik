package tls_test

import (
	"context"
	cryptotls "crypto/tls"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/module/transport/tls"
	"github.com/The-Apranik/Apranik/spec"
)

// Every name the tls page lists, so a uTLS upgrade that drops one fails here.
var everyFingerprint = []string{
	"", "none", "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized",
	"hellochrome_58", "hellochrome_62", "hellochrome_70", "hellochrome_72", "hellochrome_83", "hellochrome_87",
	"hellochrome_96", "hellochrome_100", "hellochrome_102", "hellochrome_120", "hellochrome_131", "hellochrome_133",
	"hellofirefox_55", "hellofirefox_56", "hellofirefox_63", "hellofirefox_65", "hellofirefox_99",
	"hellofirefox_102", "hellofirefox_105", "hellofirefox_120", "hellosafari_16_0",
	"helloios_11_1", "helloios_12_1", "helloios_13", "helloios_14", "helloandroid_11_okhttp",
	"helloedge_85", "hello360_7_5", "helloqq_11_1",
}

func serverConfig(t testing.TB, authority *testAuthority, alpn []string) *cryptotls.Config {
	t.Helper()
	certificatePEM, keyPEM := authority.issue(t, []string{"example.com"}, nil)
	certificate, err := cryptotls.X509KeyPair([]byte(certificatePEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	config := &cryptotls.Config{Certificates: []cryptotls.Certificate{certificate}, NextProtos: alpn}
	// Fixed, so the clone each connection gets still accepts earlier tickets.
	config.SetSessionTicketKeys([][32]byte{{1}})
	return config
}

// serveOne runs one connection from client to a crypto/tls server built from
// config, and returns the ClientHello the server received and the state it
// ended in. The server writes one byte, which the client reads, so a session
// ticket sent after the handshake reaches the client.
func serveOne(t testing.TB, config *cryptotls.Config, overlayClient spec.StreamOverlayClient) (*cryptotls.ClientHelloInfo, cryptotls.ConnectionState, error) {
	t.Helper()
	clientSide, serverSide := tcpPair(t)

	var hello *cryptotls.ClientHelloInfo
	recording := config.Clone()
	recording.GetConfigForClient = func(info *cryptotls.ClientHelloInfo) (*cryptotls.Config, error) {
		hello = info
		return nil, nil
	}
	serverDone := make(chan cryptotls.ConnectionState, 1)
	go func() {
		connection := cryptotls.Server(serverSide, recording)
		if connection.Handshake() == nil {
			connection.Write([]byte{1})
		}
		serverDone <- connection.ConnectionState()
		connection.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := overlayClient.WrapClientStream(ctx, clientSide, destination(t, "example.com", 443))
	if err != nil {
		clientSide.Close()
		<-serverDone
		return hello, cryptotls.ConnectionState{}, err
	}
	io.ReadFull(stream, make([]byte, 1))
	state := <-serverDone
	stream.Close()
	return hello, state, nil
}

func newOverlayClient(t testing.TB, client *tls.Transport) spec.StreamOverlayClient {
	t.Helper()
	overlayClient, err := client.NewStreamOverlayClient()
	if err != nil {
		t.Fatalf("NewStreamOverlayClient: %v", err)
	}
	return overlayClient
}

func helloFrom(t *testing.T, authority *testAuthority, client *tls.Transport, alpn []string) (*cryptotls.ClientHelloInfo, cryptotls.ConnectionState, error) {
	t.Helper()
	return serveOne(t, serverConfig(t, authority, alpn), newOverlayClient(t, client))
}

// Randomized ClientHellos differ per seed; each must still reach a Go server,
// which prefers the hybrid post-quantum group.
func TestEveryRandomizedSeedReachesAGoServer(t *testing.T) {
	authority := newTestAuthority(t)
	for attempt := range 50 {
		client := clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint = "randomized" })
		if _, _, err := helloFrom(t, authority, client, nil); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
}

func TestEveryFingerprintCompletesAHandshake(t *testing.T) {
	authority := newTestAuthority(t)
	for _, name := range everyFingerprint {
		client := clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint = name })
		hello, _, err := helloFrom(t, authority, client, nil)
		if hello == nil {
			t.Errorf("%q: no ClientHello reached the server: %v", name, err)
			continue
		}
		if err != nil {
			t.Errorf("%q: handshake failed: %v", name, err)
		}
	}
}

func TestAFingerprintIsNotGosOwnClientHello(t *testing.T) {
	authority := newTestAuthority(t)
	goHello, _, err := helloFrom(t, authority, clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint = "none" }), nil)
	if err != nil {
		t.Fatalf("none: %v", err)
	}
	for _, name := range []string{"", "chrome", "firefox", "safari", "randomized"} {
		hello, _, err := helloFrom(t, authority, clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint = name }), nil)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if slices.Equal(hello.CipherSuites, goHello.CipherSuites) && slices.Equal(hello.Extensions, goHello.Extensions) {
			t.Errorf("%q sent Go's own ClientHello", name)
		}
	}
}

func TestWithoutAlpnAFingerprintOffersTheBrowsersList(t *testing.T) {
	authority := newTestAuthority(t)
	hello, _, err := helloFrom(t, authority, clientTrusting(t, authority, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"h2", "http/1.1"}; !slices.Equal(hello.SupportedProtos, want) {
		t.Errorf("chrome offered %v, want %v", hello.SupportedProtos, want)
	}
}

// With alpn set, exactly that list goes out, whatever the preset built:
// under ws the other end must never be able to choose h2.
func TestAlpnReplacesEveryPresetsList(t *testing.T) {
	authority := newTestAuthority(t)
	want := []string{"http/1.1"}
	names := slices.Clone(everyFingerprint)
	for range 20 {
		names = append(names, "randomized", "random")
	}
	for _, name := range names {
		client := clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint, s.ALPN = name, want })
		hello, state, err := helloFrom(t, authority, client, []string{"h2", "http/1.1"})
		if hello == nil {
			t.Errorf("%q: no ClientHello: %v", name, err)
			continue
		}
		if !slices.Equal(hello.SupportedProtos, want) {
			t.Errorf("%q offered %v, want %v", name, hello.SupportedProtos, want)
		}
		if err == nil && state.NegotiatedProtocol != "http/1.1" {
			t.Errorf("%q: the server chose %q", name, state.NegotiatedProtocol)
		}
	}
}

// uTLS's browser presets carry no pre_shared_key extension, so a TLS 1.3
// session is never resumed with a fingerprint, in Xray and sing-box too; only
// Go's own ClientHello resumes.
func TestGosOwnClientHelloResumesItsSession(t *testing.T) {
	const name = "none"
	authority := newTestAuthority(t)
	config := serverConfig(t, authority, nil)
	client := newOverlayClient(t, clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint = name }))
	if _, _, err := serveOne(t, config, client); err != nil {
		t.Fatal(err)
	}
	_, state, err := serveOne(t, config, client)
	if err != nil {
		t.Fatal(err)
	}
	if !state.DidResume {
		t.Error("the second handshake did not resume")
	}
}

func TestADefaultFingerprintClientRefusesVersionsButAServerAcceptsThem(t *testing.T) {
	authority := newTestAuthority(t)
	client := tls.NewOverlayStreamTransportModule().(*tls.Transport)
	client.MinVersion = "1.3"
	if err := client.Validate(); err == nil {
		t.Error("Validate accepted min_version on client settings with the default fingerprint")
	}
	// Certificates a client ignores hide its role from Validate; building catches it.
	certificatePEM, keyPEM := authority.issue(t, []string{"example.com"}, nil)
	client.Certificates = []tls.Certificate{{Certificate: certificatePEM, Key: types.Secret(keyPEM)}}
	if err := client.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, err := client.NewStreamOverlayClient(); err == nil {
		t.Error("a client with the default fingerprint was built with min_version")
	}
	if _, err := clientTrusting(t, authority, func(s *tls.Settings) { s.Fingerprint, s.MinVersion = "none", "1.3" }).NewStreamOverlayClient(); err != nil {
		t.Errorf("fingerprint none with min_version: %v", err)
	}
	server := serverFor(t, authority, []string{"example.com"}, nil)
	server.MinVersion = "1.3"
	if _, err := server.NewStreamOverlayServer(); err != nil {
		t.Errorf("a server with min_version: %v", err)
	}
}

func TestTransportInfoIsWhatTheClientOffers(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*tls.Settings)
		want []string
	}{
		"nothing set: a client":     {func(*tls.Settings) {}, []string{"h2", "http/1.1"}},
		"certificates: a server":    {func(s *tls.Settings) { s.Certificates = []tls.Certificate{{Certificate: "c", Key: "k"}} }, nil},
		"alpn":                      {func(s *tls.Settings) { s.ALPN = []string{"http/1.1"} }, []string{"http/1.1"}},
		"explicit chrome":           {func(s *tls.Settings) { s.Fingerprint = "chrome" }, []string{"h2", "http/1.1"}},
		"explicit chrome with alpn": {func(s *tls.Settings) { s.Fingerprint, s.ALPN = "chrome", []string{"http/1.1"} }, []string{"http/1.1"}},
		"randomized":                {func(s *tls.Settings) { s.Fingerprint = "randomized" }, []string{"h2", "http/1.1"}},
		"none":                      {func(s *tls.Settings) { s.Fingerprint = "none" }, nil},
	} {
		transport := tls.NewOverlayStreamTransportModule().(*tls.Transport)
		tc.edit(&transport.Settings)
		if got := transport.TransportInfo().ApplicationProtocols; !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}
