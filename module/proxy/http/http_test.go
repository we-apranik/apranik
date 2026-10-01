package http_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"

	proxyhttp "github.com/The-Apranik/Apranik/module/proxy/http"
	"github.com/The-Apranik/Apranik/module/proxy/http/wire"
	"github.com/The-Apranik/Apranik/spec"
)

func domainDst(domain string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromDomain(domain, port)
	if err != nil {
		panic(err)
	}
	return destination
}

func ipDst(ip string, port uint16) spec.Destination {
	destination, err := spec.NewDestinationFromIP(netip.MustParseAddr(ip), port)
	if err != nil {
		panic(err)
	}
	return destination
}

// pipeStream adapts net.Pipe to spec.Stream. CloseWrite reports
// spec.ErrNoHalfClose, so these tests run over a stream weaker than TCP.
type pipeStream struct {
	net.Conn
}

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

func pipe(t *testing.T) (a, b spec.Stream) {
	t.Helper()

	x, y := net.Pipe()
	t.Cleanup(func() { x.Close(); y.Close() })
	return pipeStream{x}, pipeStream{y}
}

type accepted struct {
	payload spec.Stream
	meta    spec.Metadata
	err     error
}

func serverOn(t *testing.T, cfg *proxyhttp.Settings, c spec.Stream) <-chan accepted {
	t.Helper()

	srv, err := (&proxyhttp.Proxy{Settings: *cfg}).NewStreamServer()
	if err != nil {
		t.Fatalf("NewStreamServer: %v", err)
	}

	out := make(chan accepted, 1)
	go func() {
		payload, m, err := srv.AcceptStream(context.Background(), c)
		out <- accepted{payload, m, err}
	}()
	return out
}

func TestConnectRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		dst  spec.Destination
	}{
		{"domain", domainDst("example.com", 443)},
		{"IPv4", ipDst("93.184.216.34", 443)},
		{"IPv6", ipDst("2606:2800:220:1:248:1893:25c8:1946", 443)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientSide, serverSide := pipe(t)
			results := serverOn(t, &proxyhttp.Settings{}, serverSide)

			cl, err := (&proxyhttp.Proxy{Settings: proxyhttp.Settings{}}).NewStreamClient()
			if err != nil {
				t.Fatalf("NewStreamClient: %v", err)
			}

			up, err := cl.RequestStream(context.Background(), clientSide, spec.Metadata{
				Network:     spec.NetworkTCP,
				Destination: tc.dst,
			})
			if err != nil {
				t.Fatalf("RequestStream: %v", err)
			}

			got := <-results
			if got.err != nil {
				t.Fatalf("AcceptStream: %v", got.err)
			}
			if got.meta.Destination != tc.dst {
				t.Errorf("server saw %+v, client sent %+v", got.meta.Destination, tc.dst)
			}

			go up.Write([]byte("payload"))
			buf := make([]byte, len("payload"))
			if _, err := io.ReadFull(got.payload, buf); err != nil {
				t.Fatalf("reading payload: %v", err)
			}
			if string(buf) != "payload" {
				t.Errorf("payload = %q, want %q", buf, "payload")
			}
		})
	}
}

// A client that pipelines its first payload straight after CONNECT is normal:
// curl sends the TLS ClientHello without waiting. The bytes must survive the
// reader used to parse the head.
func TestPipelinedPayloadSurvivesTheHandshake(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serverOn(t, &proxyhttp.Settings{}, serverSide)

	go clientSide.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\nCLIENTHELLO"))

	br := bufio.NewReader(clientSide)
	if status, err := br.ReadString('\n'); err != nil || !strings.Contains(status, "200") {
		t.Fatalf("status = %q, err = %v", status, err)
	}

	got := <-results
	if got.err != nil {
		t.Fatalf("AcceptStream: %v", got.err)
	}

	buf := make([]byte, len("CLIENTHELLO"))
	if _, err := io.ReadFull(got.payload, buf); err != nil {
		t.Fatalf("reading pipelined payload: %v", err)
	}
	if string(buf) != "CLIENTHELLO" {
		t.Errorf("payload = %q, want %q: bytes read while parsing the head were lost", buf, "CLIENTHELLO")
	}
}

func TestAbsoluteFormIsRewrittenForTheOrigin(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serverOn(t, &proxyhttp.Settings{}, serverSide)

	go clientSide.Write([]byte("GET http://example.com/path?q=1 HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Proxy-Connection: keep-alive\r\n" +
		"Proxy-Authorization: Basic zzz\r\n" +
		"User-Agent: apranik-test\r\n\r\n"))

	got := <-results
	if got.err != nil {
		t.Fatalf("AcceptStream: %v", got.err)
	}
	if got.meta.Destination != domainDst("example.com", 80) {
		t.Errorf("destination = %+v, want example.com:80", got.meta.Destination)
	}

	head := make([]byte, 512)
	n, err := got.payload.Read(head)
	if err != nil {
		t.Fatalf("reading the rewritten request: %v", err)
	}
	forwarded := string(head[:n])

	if !strings.HasPrefix(forwarded, "GET /path?q=1 HTTP/1.1\r\n") {
		t.Errorf("request line not rewritten to origin form:\n%q", forwarded)
	}
	for _, gone := range []string{"Proxy-Connection", "Proxy-Authorization"} {
		if strings.Contains(forwarded, gone) {
			t.Errorf("%s was forwarded to the origin:\n%q", gone, forwarded)
		}
	}
	if !strings.Contains(forwarded, "User-Agent: apranik-test") {
		t.Errorf("end-to-end header dropped:\n%q", forwarded)
	}
	if !strings.Contains(forwarded, "Connection: close") {
		t.Errorf("Connection: close not forced; a keep-alive request for another host would go to this origin:\n%q", forwarded)
	}
}

func TestProxyAuthentication(t *testing.T) {
	const user, pass = "researcher", "correct horse"

	t.Run("accepted", func(t *testing.T) {
		clientSide, serverSide := pipe(t)
		results := serverOn(t, &proxyhttp.Settings{Username: user, Password: pass}, serverSide)

		cl, err := (&proxyhttp.Proxy{Settings: proxyhttp.Settings{Username: user, Password: pass}}).NewStreamClient()
		if err != nil {
			t.Fatalf("NewStreamClient: %v", err)
		}
		if _, err := cl.RequestStream(context.Background(), clientSide, spec.Metadata{
			Network:     spec.NetworkTCP,
			Destination: domainDst("example.com", 443),
		}); err != nil {
			t.Fatalf("RequestStream: %v", err)
		}

		got := <-results
		if got.err != nil {
			t.Fatalf("AcceptStream: %v", got.err)
		}
		if got.meta.User != user {
			t.Errorf("Metadata.User = %q, want %q", got.meta.User, user)
		}
	})

	t.Run("rejected with 407", func(t *testing.T) {
		clientSide, serverSide := pipe(t)
		results := serverOn(t, &proxyhttp.Settings{Username: user, Password: pass}, serverSide)

		go clientSide.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))

		br := bufio.NewReader(clientSide)
		status, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("reading status: %v", err)
		}
		if !strings.Contains(status, "407") {
			t.Errorf("status = %q, want 407", status)
		}
		if got := <-results; got.err == nil {
			t.Fatal("AcceptStream accepted an unauthenticated client")
		}
	})

	t.Run("client surfaces a refusal", func(t *testing.T) {
		clientSide, serverSide := pipe(t)

		go func() {
			br := bufio.NewReader(serverSide)
			for {
				line, err := br.ReadString('\n')
				if err != nil || line == "\r\n" {
					break
				}
			}
			serverSide.Write([]byte("HTTP/1.1 403 Forbidden\r\n\r\n"))
		}()

		cl, err := (&proxyhttp.Proxy{Settings: proxyhttp.Settings{}}).NewStreamClient()
		if err != nil {
			t.Fatalf("NewStreamClient: %v", err)
		}
		_, err = cl.RequestStream(context.Background(), clientSide, spec.Metadata{
			Network:     spec.NetworkTCP,
			Destination: domainDst("example.com", 443),
		})

		var status *wire.ConnectRefusedError
		if !errors.As(err, &status) {
			t.Fatalf("RequestStream error is %v, want *wire.ConnectRefusedError", err)
		}
		if status.StatusCode != 403 {
			t.Errorf("StatusCode = %d, want 403", status.StatusCode)
		}
	})
}

func TestMalformedRequestGets400(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serverOn(t, &proxyhttp.Settings{}, serverSide)

	go clientSide.Write([]byte("GET /relative HTTP/1.1\r\nHost: example.com\r\n\r\n"))

	br := bufio.NewReader(clientSide)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if !strings.Contains(status, "400") {
		t.Errorf("status = %q, want 400: origin-form is not a proxy request", status)
	}
	if got := <-results; got.err == nil {
		t.Fatal("AcceptStream accepted an origin-form request")
	}
}

func TestClientRefusesUDPMetadata(t *testing.T) {
	clientSide, _ := pipe(t)

	cl, err := (&proxyhttp.Proxy{Settings: proxyhttp.Settings{}}).NewStreamClient()
	if err != nil {
		t.Fatalf("NewStreamClient: %v", err)
	}

	_, err = cl.RequestStream(context.Background(), clientSide, spec.Metadata{
		Network:     spec.NetworkUDP,
		Destination: domainDst("example.com", 53),
	})
	if !errors.Is(err, spec.ErrUnsupportedNetwork) {
		t.Errorf("RequestStream with UDP metadata: %v, want spec.ErrUnsupportedNetwork", err)
	}
}

func TestModuleFillsOneShapeOnly(t *testing.T) {
	var m any = (&proxyhttp.Proxy{})

	if _, ok := m.(spec.StreamProxyModule); !ok {
		t.Error("http.Module does not implement spec.StreamProxyModule")
	}
	if _, ok := m.(spec.DatagramProxyModule); ok {
		t.Error("http.Module implements spec.DatagramProxyModule; HTTP has no UDP story")
	}
	if _, ok := m.(spec.DatagramOverStreamProxyModule); ok {
		t.Error("http.Module implements spec.DatagramOverStreamProxyModule")
	}
}

func TestValidateRefusesHalfCredentials(t *testing.T) {
	if err := (&proxyhttp.Settings{Username: "u"}).Validate(); err == nil {
		t.Error("a username with no password was accepted, want an error")
	}
	if err := (&proxyhttp.Settings{Password: "p"}).Validate(); err == nil {
		t.Error("a password with no username was accepted, want an error")
	}
}

func TestSettingsDoNotPrintTheirPassword(t *testing.T) {
	settings := proxyhttp.Settings{Username: "u", Password: "hunter2"}

	for _, rendered := range []string{
		fmt.Sprintf("%v", settings),
		fmt.Sprintf("%+v", settings),
		fmt.Sprintf("%#v", settings),
	} {
		if strings.Contains(rendered, "hunter2") {
			t.Errorf("settings leaked the password: %s", rendered)
		}
	}
}

// Basic auth is base64, not encryption: over a bare TCP carrier the credential
// is recoverable by anyone on the path.
func TestBasicAuthIsWarnedAbout(t *testing.T) {
	warnings := (&proxyhttp.Settings{Username: "u", Password: "p"}).Warnings()
	if len(warnings) == 0 {
		t.Fatal("configured Basic auth produced no warning")
	}

	var mentionsEncryption bool
	for _, warning := range warnings {
		if strings.Contains(warning, "encrypted") {
			mentionsEncryption = true
		}
	}
	if !mentionsEncryption {
		t.Errorf("warnings do not mention the carrier: %v", warnings)
	}
}

func TestDecodeFillsPromotedFields(t *testing.T) {
	module := proxyhttp.NewStreamProxyModule()

	decoder := json.NewDecoder(strings.NewReader(`{"username":"alice","password":"hunter2"}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(module); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	proxy := module.(*proxyhttp.Proxy)
	if proxy.Username != "alice" || proxy.Password.Reveal() != "hunter2" {
		t.Errorf("decode did not fill the promoted fields: %q", proxy.Username)
	}
}

func BenchmarkAcceptConnect(b *testing.B) {
	server, err := (&proxyhttp.Proxy{}).NewStreamServer()
	if err != nil {
		b.Fatal(err)
	}
	request := []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	b.ReportAllocs()
	for b.Loop() {
		clientSide, serverSide := net.Pipe()
		go func() {
			clientSide.Write(request)
			io.Copy(io.Discard, clientSide)
		}()
		if _, _, err := server.AcceptStream(context.Background(), pipeStream{serverSide}); err != nil {
			b.Fatal(err)
		}
		serverSide.Close()
		clientSide.Close()
	}
}
