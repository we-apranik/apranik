package builder_test

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// testZone answers A queries from a table, as a recursive resolver would,
// and counts what it was asked, so a test can tell its lookups went there.
type testZone struct {
	addresses map[string]netip.Addr
	queries   atomic.Int64
}

func (z *testZone) answer(query []byte) []byte {
	z.queries.Add(1)
	var parser dnsmessage.Parser
	header, err := parser.Start(query)
	if err != nil {
		return nil
	}
	question, err := parser.Question()
	if err != nil {
		return nil
	}
	response := dnsmessage.Header{ID: header.ID, Response: true, RecursionAvailable: true}
	address, known := z.addresses[strings.ToLower(question.Name.String())]
	if !known {
		response.RCode = dnsmessage.RCodeNameError
	}
	builder := dnsmessage.NewBuilder(nil, response)
	builder.StartQuestions()
	builder.Question(question)
	builder.StartAnswers()
	if known && question.Type == dnsmessage.TypeA {
		builder.AResource(dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.AResource{A: address.As4()})
	}
	message, _ := builder.Finish()
	return message
}

func startUDPDNSServer(t *testing.T, zone *testZone) string {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	go func() {
		buffer := make([]byte, 2048)
		for {
			length, from, err := connection.ReadFromUDPAddrPort(buffer)
			if err != nil {
				return
			}
			connection.WriteToUDPAddrPort(zone.answer(buffer[:length]), from)
		}
	}()
	return connection.LocalAddr().String()
}

// startStreamDNSServer serves DNS over TCP, or over TLS when tlsConfig is
// set: length-prefixed messages on a connection kept open.
func startStreamDNSServer(t *testing.T, zone *testZone, tlsConfig *tls.Config) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				for {
					var prefix [2]byte
					if _, err := io.ReadFull(connection, prefix[:]); err != nil {
						return
					}
					query := make([]byte, binary.BigEndian.Uint16(prefix[:]))
					if _, err := io.ReadFull(connection, query); err != nil {
						return
					}
					response := zone.answer(query)
					connection.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(response))), response...))
				}
			}()
		}
	}()
	return listener.Addr().String()
}

// startDoHServer is an RFC 8484 server over TLS; its certificate also serves
// the DoT server, and the client trusts it through certificate_authorities.
func startDoHServer(t *testing.T, zone *testZone) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/dns-query" || request.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(writer, "not a DoH request", http.StatusBadRequest)
			return
		}
		query, _ := io.ReadAll(io.LimitReader(request.Body, 65535))
		writer.Header().Set("Content-Type", "application/dns-message")
		writer.Write(zone.answer(query))
	}))
	t.Cleanup(server.Close)
	return server
}

func trustedTLSBlock(t *testing.T, server *httptest.Server, alpn string) string {
	t.Helper()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	quoted, _ := json.Marshal(string(certificate))
	block := `{"server_name": "example.com", "certificate_authorities": [{"certificate": ` + string(quoted) + `}]`
	if alpn != "" {
		block += `, "alpn": ["` + alpn + `"]`
	}
	return block + `}`
}

// dnsRoutedConfig is a SOCKS5 inbound and one freedom outbound, resolving
// through the given dns section, with the given routing.
func dnsRoutedConfig(proxyPort uint16, dns, routing string) string {
	document := fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
		}],
		"outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}],
		"dns": %s`, proxyPort, dns)
	if routing != "" {
		document += `, "routing": ` + routing
	}
	return document + `}`
}

// Freedom dials a domain through each DNS server type in turn, and the
// lookup is seen to reach that server.
func TestFreedomResolvesThroughEachDNSServerType(t *testing.T) {
	zone := &testZone{addresses: map[string]netip.Addr{"app.test.": netip.MustParseAddr("127.0.0.1")}}
	doh := startDoHServer(t, zone)
	tlsConfig := doh.TLS.Clone()
	tlsConfig.NextProtos = nil
	echoPort := startTCPEchoServer(t)

	for _, tc := range []struct{ name, server string }{
		{"udp", fmt.Sprintf(`{"tag": "s", "type": "udp", "settings": {"server": %q}, "outbound": "direct"}`, startUDPDNSServer(t, zone))},
		{"tcp", fmt.Sprintf(`{"tag": "s", "type": "tcp", "settings": {"server": %q}}`, startStreamDNSServer(t, zone, nil))},
		{"dot", fmt.Sprintf(`{"tag": "s", "type": "dot", "settings": {"server": %q}, "tls": %s, "outbound": "direct"}`,
			startStreamDNSServer(t, zone, tlsConfig), trustedTLSBlock(t, doh, ""))},
		{"doh", fmt.Sprintf(`{"tag": "s", "type": "doh", "settings": {"server": %q, "host": "example.com"}, "tls": %s}`,
			strings.TrimPrefix(doh.URL, "https://"), trustedTLSBlock(t, doh, "http/1.1"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := zone.queries.Load()
			proxyPort := freeLoopbackPort(t)
			proxy := startRoutedSOCKS5(t, t.TempDir(), dnsRoutedConfig(proxyPort,
				`{"servers": [`+tc.server+`], "default_server": "s", "ip_versions": "ipv4_only"}`, ""))

			connection := socks5Connect(t, proxy, "app.test", echoPort)
			if !echoed(t, connection, "through "+tc.name) {
				t.Fatal("the connection to a resolved name was not relayed")
			}
			if zone.queries.Load() == before {
				t.Fatalf("the %s server was never asked", tc.name)
			}
		})
	}
}

// A resolve action lets an IP rule decide a domain destination.
func TestResolveActionFeedsIPRules(t *testing.T) {
	zone := &testZone{addresses: map[string]netip.Addr{
		"app.test.":     netip.MustParseAddr("127.0.0.1"),
		"blocked.test.": netip.MustParseAddr("127.0.0.2"),
	}}
	echoPort := startTCPEchoServer(t)
	proxyPort := freeLoopbackPort(t)
	proxy := startRoutedSOCKS5(t, t.TempDir(), dnsRoutedConfig(proxyPort,
		fmt.Sprintf(`{"servers": [{"tag": "s", "type": "udp", "settings": {"server": %q}}], "default_server": "s", "ip_versions": "ipv4_only"}`, startUDPDNSServer(t, zone)),
		`{"rules": [
			{"match": {"network": "tcp"}, "action": {"type": "resolve"}},
			{"match": {"ip": ["127.0.0.2/32"]}, "action": {"type": "block"}}
		], "default_action": {"type": "route", "settings": {"outbound": "direct"}}}`))

	if !echoed(t, socks5Connect(t, proxy, "app.test", echoPort), "allowed") {
		t.Fatal("a name whose address no rule blocks was not relayed")
	}

	// SOCKS5 accepts before routing; a block closes the stream after.
	if echoed(t, socks5Connect(t, proxy, "blocked.test", echoPort), "blocked") {
		t.Fatal("a name resolving to a blocked address got through")
	}
}
