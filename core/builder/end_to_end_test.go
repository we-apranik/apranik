package builder_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/spec"
)

// End-to-end tests run a Trojan server and client built from config files,
// as `apranik run` builds them, over a given transport stack. This file holds
// the helpers every stack's tests share, and the tests for TLS; the other
// overlays' tests sit in their own files.

const proxyServerName = "proxy.example"

type testCertificates struct {
	authorityPEM string
	leafPEM      string
	leafKeyPEM   string
	authority    *x509.CertPool
}

func newTestCertificates(t *testing.T) testCertificates {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	encode := func(blockType string, der []byte) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
	}

	authorityKey := newKey()
	authorityTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	authorityDER, err := x509.CreateCertificate(rand.Reader, authorityTemplate, authorityTemplate, &authorityKey.PublicKey, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	authorityCertificate, err := x509.ParseCertificate(authorityDER)
	if err != nil {
		t.Fatal(err)
	}

	leafKey := newKey()
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: proxyServerName},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{proxyServerName},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, authorityCertificate, &leafKey.PublicKey, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(authorityCertificate)
	return testCertificates{
		authorityPEM: encode("CERTIFICATE", authorityDER),
		leafPEM:      encode("CERTIFICATE", leafDER),
		leafKeyPEM:   encode("EC PRIVATE KEY", leafKeyDER),
		authority:    pool,
	}
}

func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func parseConfig(t *testing.T, text string) *blueprint.Blueprint {
	t.Helper()
	modules, err := registry.Default()
	if err != nil {
		t.Fatal(err)
	}
	parsed, diagnostics := config.Parse([]byte(text), modules)
	if diagnostics.HasErrors() {
		t.Fatalf("config refused: %v", diagnostics)
	}
	if more := config.Check(parsed); more.HasErrors() {
		t.Fatalf("config refused: %v", more)
	}
	return parsed
}

func tlsServerTransport(t *testing.T, certificates testCertificates) string {
	return fmt.Sprintf(`[
		{ "type": "tcp" },
		{ "type": "tls", "settings": { "certificates": [{ "certificate": %s, "key": %s }] } }
	]`, jsonString(t, certificates.leafPEM), jsonString(t, certificates.leafKeyPEM))
}

func tlsClientTransport(t *testing.T, certificates testCertificates) string {
	return fmt.Sprintf(`[
		{ "type": "tcp" },
		{ "type": "tls", "settings": { "server_name": %q, "alpn": ["http/1.1"], "certificate_authorities": [{ "certificate": %s }] } }
	]`, proxyServerName, jsonString(t, certificates.authorityPEM))
}

// startServer runs a Trojan-over-TLS inbound that sends everything out
// through freedom, and a fallback that records what it is sent.
func startServer(t *testing.T, certificates testCertificates) (port uint16, fallbackReceived <-chan []byte) {
	return startTrojanServer(t, tlsServerTransport(t, certificates))
}

// startTrojanServer is startServer over any transport stack, given as JSON.
func startTrojanServer(t *testing.T, transport string) (port uint16, fallbackReceived <-chan []byte) {
	t.Helper()
	fallback, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fallback.Close() })
	received := make(chan []byte, 1)
	go func() {
		connection, err := fallback.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		connection.SetReadDeadline(time.Now().Add(5 * time.Second))
		buffer := make([]byte, 1024)
		n, _ := connection.Read(buffer)
		received <- buffer[:n]
	}()

	port = freeLoopbackPort(t)
	serverBlueprint := parseConfig(t, fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "trojan-tls-in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
			"handshake_timeout": "300ms",
			"transport": %s,
			"protocol": { "type": "trojan", "settings": { "users": [{ "name": "alice", "password": "correct horse" }] } },
			"fallback": { "server": "127.0.0.1", "server_port": %d }
		}],
		"outbounds": [{ "tag": "freedom", "protocol": { "type": "freedom" } }]
	}`, port, transport, fallback.Addr().(*net.TCPAddr).Port))

	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })
	freedom, err := built.BuildOutbound(serverBlueprint.Outbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { freedom.Close() })
	inbound, err := built.BuildInbound(serverBlueprint.Inbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inbound.Close() })
	if err := inbound.Activate(context.Background(), handler.New(router.New(freedom), handler.Config{})); err != nil {
		t.Fatal(err)
	}
	return port, received
}

func buildClientOutbound(t *testing.T, certificates testCertificates, serverPort uint16) component.Outbound {
	return buildTrojanClient(t, tlsClientTransport(t, certificates), serverPort)
}

// buildTrojanClient is buildClientOutbound over any transport stack.
func buildTrojanClient(t *testing.T, transport string, serverPort uint16) component.Outbound {
	t.Helper()
	clientBlueprint := parseConfig(t, fmt.Sprintf(`{
		"version": 1,
		"outbounds": [{
			"tag": "trojan-out", "server": "127.0.0.1", "server_port": %d,
			"transport": %s,
			"protocol": { "type": "trojan", "settings": { "password": "correct horse" } }
		}]
	}`, serverPort, transport))

	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })
	outbound, err := built.BuildOutbound(clientBlueprint.Outbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { outbound.Close() })
	return outbound
}

func loopbackDestination(t *testing.T, port int) spec.Destination {
	t.Helper()
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr("127.0.0.1"), uint16(port))
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func TestTrojanOverTLSCarriesTCP(t *testing.T) {
	certificates := newTestCertificates(t)
	serverPort, _ := startServer(t, certificates)
	checkTCPThrough(t, buildClientOutbound(t, certificates, serverPort))
}

// checkTCPThrough echoes a message through outbound to a local TCP server.
func checkTCPThrough(t *testing.T, outbound component.Outbound) {
	t.Helper()
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		connection, err := echo.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		io.Copy(connection, connection)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := outbound.DialStream(ctx, spec.Metadata{
		Network:     spec.NetworkTCP,
		Destination: loopbackDestination(t, echo.Addr().(*net.TCPAddr).Port),
	})
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := stream.Write([]byte("hello through tls")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	reply := make([]byte, len("hello through tls"))
	if _, err := io.ReadFull(stream, reply); err != nil || string(reply) != "hello through tls" {
		t.Fatalf("echo returned %q, %v", reply, err)
	}
}

func TestTrojanOverTLSCarriesUDP(t *testing.T) {
	certificates := newTestCertificates(t)
	serverPort, _ := startServer(t, certificates)
	checkUDPThrough(t, buildClientOutbound(t, certificates, serverPort))
}

// checkUDPThrough echoes a datagram through outbound to a local UDP server.
func checkUDPThrough(t *testing.T, outbound component.Outbound) {
	t.Helper()
	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, from, err := echo.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			echo.WriteToUDP(buffer[:n], from)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	datagram, err := outbound.CreateDatagram(ctx, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer datagram.Close()

	destination := loopbackDestination(t, echo.LocalAddr().(*net.UDPAddr).Port)
	if _, err := datagram.WriteTo([]byte("a datagram"), destination); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	type readResult struct {
		payload []byte
		from    spec.Destination
		err     error
	}
	done := make(chan readResult, 1)
	go func() {
		buffer := make([]byte, 2048)
		n, from, err := datagram.ReadFrom(buffer)
		done <- readResult{buffer[:n], from, err}
	}()
	select {
	case result := <-done:
		if result.err != nil || string(result.payload) != "a datagram" || result.from != destination {
			t.Fatalf("read %q from %s, %v", result.payload, result.from, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
}

// A client that completes TLS but sends no valid Trojan request is handed to
// the fallback with the bytes it sent inside TLS, not TLS records.
func TestTheFallbackReceivesWhatWasSentInsideTLS(t *testing.T) {
	certificates := newTestCertificates(t)
	serverPort, fallbackReceived := startServer(t, certificates)

	connection, err := cryptotls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", serverPort), &cryptotls.Config{
		ServerName: proxyServerName, RootCAs: certificates.authority,
	})
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	defer connection.Close()
	probe := []byte("GET / HTTP/1.1\r\nHost: " + proxyServerName + "\r\n\r\n" + string(bytes.Repeat([]byte("x"), 64)))
	connection.Write(probe)

	select {
	case got := <-fallbackReceived:
		if !bytes.Equal(got, probe) {
			t.Errorf("fallback received %q, want %q", got, probe)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fallback received nothing")
	}
}

// The TLS handshake runs per connection, so a client that connects and says
// nothing is closed by the handshake timeout and holds up nobody else.
func TestASilentClientDoesNotDelayAnother(t *testing.T) {
	certificates := newTestCertificates(t)
	serverPort, _ := startServer(t, certificates)
	address := fmt.Sprintf("127.0.0.1:%d", serverPort)

	silent, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()

	start := time.Now()
	connection, err := cryptotls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", address, &cryptotls.Config{
		ServerName: proxyServerName, RootCAs: certificates.authority,
	})
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	connection.Close()
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("the second client's handshake took %s behind a silent one", waited)
	}

	silent.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := silent.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("the silent client read %v, want EOF from the handshake timeout", err)
	}
}
