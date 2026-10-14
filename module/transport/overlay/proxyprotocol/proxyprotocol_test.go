package proxyprotocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/we-apranik/apranik/spec"
)

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
	t.Cleanup(func() { clientSide.Close(); serverSide.Close() })
	return clientSide, serverSide
}

func endpoint(t testing.TB, address string, port uint16) spec.Endpoint {
	t.Helper()
	result, err := spec.NewEndpoint(netip.MustParseAddr(address), port)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func module(t testing.TB, settingsJSON string) *Transport {
	t.Helper()
	transport := NewOverlayStreamTransportModule().(*Transport)
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &transport.Settings); err != nil {
			t.Fatal(err)
		}
	}
	if err := transport.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return transport
}

// wrap sends what a proxy would write to a server whose peer is at peer, and
// returns the stream and metadata the overlay produced.
func wrap(t testing.TB, transport *Transport, peer string, sent []byte, closeAfterSending bool) (spec.Stream, *spec.TransportMetadata, error) {
	t.Helper()
	overlayServer, err := transport.NewStreamOverlayServer()
	if err != nil {
		t.Fatal(err)
	}
	clientSide, serverSide := tcpPair(t)
	metadata := &spec.TransportMetadata{SourceEndpoint: endpoint(t, peer, 4000)}
	if len(sent) > 0 {
		if _, err := clientSide.Write(sent); err != nil {
			t.Fatal(err)
		}
	}
	if closeAfterSending {
		clientSide.CloseWrite()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := overlayServer.WrapServerStream(ctx, serverSide, metadata)
	return stream, metadata, err
}

func versionTwo(command, familyAndProtocol byte, body []byte) []byte {
	header := append([]byte{}, versionTwoSignature...)
	header = append(header, 0x20|command, familyAndProtocol, byte(len(body)>>8), byte(len(body)))
	return append(header, body...)
}

func inet4Body(source, destination string, sourcePort, destinationPort uint16) []byte {
	body := append(netip.MustParseAddr(source).AsSlice(), netip.MustParseAddr(destination).AsSlice()...)
	return binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(body, sourcePort), destinationPort)
}

func inet6Body(source, destination string, sourcePort, destinationPort uint16) []byte {
	body := append(netip.MustParseAddr(source).AsSlice(), netip.MustParseAddr(destination).AsSlice()...)
	return binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(body, sourcePort), destinationPort)
}

func TestTheClientNamedByAHeaderBecomesTheSource(t *testing.T) {
	for name, tc := range map[string]struct {
		sent            []byte
		wantVersion     int
		wantSource      string
		wantDestination string
	}{
		"v1 TCP4":              {[]byte("PROXY TCP4 192.0.2.1 198.51.100.1 56324 443\r\n"), 1, "192.0.2.1:56324", "198.51.100.1:443"},
		"v1 TCP6":              {[]byte("PROXY TCP6 2001:db8::1 2001:db8::2 56324 443\r\n"), 1, "[2001:db8::1]:56324", "[2001:db8::2]:443"},
		"v2 TCP4":              {versionTwo(1, 0x11, inet4Body("192.0.2.1", "198.51.100.1", 56324, 443)), 2, "192.0.2.1:56324", "198.51.100.1:443"},
		"v2 TCP6":              {versionTwo(1, 0x21, inet6Body("2001:db8::1", "2001:db8::2", 56324, 443)), 2, "[2001:db8::1]:56324", "[2001:db8::2]:443"},
		"v2 with TLVs skipped": {versionTwo(1, 0x11, append(inet4Body("192.0.2.1", "198.51.100.1", 56324, 443), 0x01, 0x00, 0x03, 'h', '2', 'x')), 2, "192.0.2.1:56324", "198.51.100.1:443"},
	} {
		t.Run(name, func(t *testing.T) {
			_, metadata, err := wrap(t, module(t, ""), "127.0.0.1", tc.sent, false)
			if err != nil {
				t.Fatalf("WrapServerStream: %v", err)
			}
			if metadata.SourceEndpoint.String() != tc.wantSource {
				t.Errorf("SourceEndpoint = %s, want %s", metadata.SourceEndpoint, tc.wantSource)
			}
			section := metadata.ProxyProtocol
			if section == nil || section.Version != tc.wantVersion || section.SourceEndpoint.String() != tc.wantSource || section.DestinationEndpoint.String() != tc.wantDestination {
				t.Errorf("ProxyProtocol = %+v, want version %d, %s to %s", section, tc.wantVersion, tc.wantSource, tc.wantDestination)
			}
			if metadata.InboundRemoteEndpoint.IsValid() {
				t.Errorf("the overlay touched InboundRemoteEndpoint: %s", metadata.InboundRemoteEndpoint)
			}
		})
	}
}

func TestAHeaderThatNamesNoClientKeepsThePeer(t *testing.T) {
	for name, sent := range map[string][]byte{
		"v1 UNKNOWN":      []byte("PROXY UNKNOWN\r\n"),
		"v1 UNKNOWN junk": []byte("PROXY UNKNOWN ignored 1 2 3\r\n"),
		"v2 LOCAL":        versionTwo(0, 0x00, nil),
		"v2 UNSPEC":       versionTwo(1, 0x00, nil),
		"v2 unix":         versionTwo(1, 0x31, make([]byte, 216)),
		"v2 datagram":     versionTwo(1, 0x12, inet4Body("192.0.2.1", "198.51.100.1", 1, 2)),
	} {
		t.Run(name, func(t *testing.T) {
			_, metadata, err := wrap(t, module(t, ""), "127.0.0.1", sent, false)
			if err != nil {
				t.Fatalf("WrapServerStream: %v", err)
			}
			if metadata.SourceEndpoint.Addr() != netip.MustParseAddr("127.0.0.1") || metadata.ProxyProtocol != nil {
				t.Errorf("Source = %s, ProxyProtocol = %+v; want the peer and no section", metadata.SourceEndpoint, metadata.ProxyProtocol)
			}
		})
	}
}

func TestTheClientsOwnBytesAfterTheHeaderArriveUntouched(t *testing.T) {
	for name, header := range map[string][]byte{
		"v1": []byte("PROXY TCP4 192.0.2.1 198.51.100.1 56324 443\r\n"),
		"v2": versionTwo(1, 0x11, inet4Body("192.0.2.1", "198.51.100.1", 56324, 443)),
	} {
		t.Run(name, func(t *testing.T) {
			payload := "\x16\x03\x01 a TLS ClientHello would start here"
			stream, _, err := wrap(t, module(t, ""), "127.0.0.1", append(header, payload...), false)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(stream, got); err != nil || string(got) != payload {
				t.Errorf("read %q, %v; want %q", got, err, payload)
			}
		})
	}
}

func TestAPeerOutsideTheListIsRefusedWithAMessageThatSaysWhy(t *testing.T) {
	_, _, err := wrap(t, module(t, ""), "203.0.113.9", []byte("PROXY TCP4 192.0.2.1 198.51.100.1 1 2\r\n"), false)
	if err == nil {
		t.Fatal("a peer outside the default list was accepted")
	}
	for _, want := range []string{"203.0.113.9", "trusted_proxy_addresses", "directly after the base transport"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestTheDefaultListIsLoopbackAndAListedPeerIsAccepted(t *testing.T) {
	sent := []byte("PROXY TCP4 192.0.2.1 198.51.100.1 1 2\r\n")
	for _, peer := range []string{"127.0.0.1", "127.9.9.9", "::1"} {
		if _, _, err := wrap(t, module(t, ""), peer, sent, false); err != nil {
			t.Errorf("loopback peer %s refused: %v", peer, err)
		}
	}
	custom := module(t, `{"verification": {"trusted_proxy_addresses": ["10.0.0.0/8"]}}`)
	if _, _, err := wrap(t, custom, "10.1.2.3", sent, false); err != nil {
		t.Errorf("a listed peer was refused: %v", err)
	}
	if _, _, err := wrap(t, custom, "127.0.0.1", sent, false); err == nil {
		t.Error("loopback was accepted although the list replaced the default")
	}
}

func TestABadHeaderClosesTheConnectionWithAnExplanation(t *testing.T) {
	for name, sent := range map[string][]byte{
		"not a header":              []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
		"a TLS handshake":           append([]byte{0x16, 0x03, 0x01, 0x02, 0x00, 0x01, 0x00, 0x01, 0xfc, 0x03, 0x03}, make([]byte, 20)...),
		"v1 wrong family":           []byte("PROXY TCP4 2001:db8::1 2001:db8::2 1 2\r\n"),
		"v1 bad port":               []byte("PROXY TCP4 192.0.2.1 198.51.100.1 99999 2\r\n"),
		"v1 missing fields":         []byte("PROXY TCP4 192.0.2.1\r\n\r\n"),
		"v1 line too long":          append([]byte("PROXY TCP4 "), []byte(strings.Repeat("1", 200))...),
		"v1 ends too early":         []byte("PROXY \r\n and then some bytes"),
		"v2 wrong version":          append(append([]byte{}, versionTwoSignature...), 0x11, 0x11, 0, 12),
		"v2 oversized":              versionTwo(1, 0x11, make([]byte, 5000)),
		"v2 too short for the IPv4": versionTwo(1, 0x11, make([]byte, 4)),
		"v2 too short for the IPv6": versionTwo(1, 0x21, make([]byte, 20)),
		"v2 bad command":            versionTwo(7, 0x11, inet4Body("192.0.2.1", "198.51.100.1", 1, 2)),
		"truncated v1":              []byte("PROXY TCP4 192.0.2.1 198.51"),
		"truncated v2":              append(append([]byte{}, versionTwoSignature...), 0x21, 0x11),
		"nothing, then closed":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := wrap(t, module(t, `{"timeout": "2s"}`), "127.0.0.1", sent, true)
			if err == nil {
				t.Fatal("a bad header was accepted")
			}
			for _, want := range []string{"127.0.0.1", "sent no valid PROXY header", "directly after the base transport"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestASilentPeerIsCutOffAtTheTimeout(t *testing.T) {
	started := time.Now()
	_, _, err := wrap(t, module(t, `{"timeout": "100ms"}`), "127.0.0.1", nil, false)
	if err == nil || !strings.Contains(err.Error(), "100ms") {
		t.Fatalf("error = %v, want one that names the timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("took %s to give up on a 100ms timeout", elapsed)
	}
}

func TestSettings(t *testing.T) {
	for name, tc := range map[string]struct {
		settings string
		wantErr  string
	}{
		"defaults":           {`{}`, ""},
		"an empty list":      {`{"verification": {"trusted_proxy_addresses": []}}`, "empty"},
		"a negative timeout": {`{"timeout": "-1s"}`, "greater than zero"},
		"a zero timeout":     {`{"timeout": "0s"}`, "greater than zero"},
		"a bad address":      {`{"verification": {"trusted_proxy_addresses": ["nope"]}}`, "decode"},
	} {
		t.Run(name, func(t *testing.T) {
			transport := NewOverlayStreamTransportModule().(*Transport)
			err := json.Unmarshal([]byte(tc.settings), &transport.Settings)
			if err == nil {
				err = transport.Validate()
			}
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.wantErr == "decode" && err == nil:
				t.Fatal("a bad address was accepted")
			case tc.wantErr != "" && tc.wantErr != "decode" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}

	defaults := NewOverlayStreamTransportModule().(*Transport)
	if defaults.timeout() != time.Second || len(defaults.Warnings()) != 0 {
		t.Errorf("default timeout %s, warnings %v", defaults.timeout(), defaults.Warnings())
	}
	broad := module(t, `{"verification": {"trusted_proxy_addresses": ["0.0.0.0/0", "::/0"]}}`)
	if len(broad.Warnings()) != 1 {
		t.Errorf("a list covering every address warned %v", broad.Warnings())
	}
}

func TestThereIsNoClientSide(t *testing.T) {
	if _, err := module(t, "").NewStreamOverlayClient(); !errors.Is(err, errNoClientSide) {
		t.Errorf("NewStreamOverlayClient error = %v, want errNoClientSide", err)
	}
}
