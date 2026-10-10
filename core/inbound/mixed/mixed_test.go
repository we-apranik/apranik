package mixed_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/dns"
	"github.com/The-Apranik/Apranik/core/inbound"
	"github.com/The-Apranik/Apranik/core/inbound/mixed"
	proxyhttp "github.com/The-Apranik/Apranik/module/proxy/http"
	"github.com/The-Apranik/Apranik/module/proxy/socks5"
	"github.com/The-Apranik/Apranik/module/transport/base/tcp"
	"github.com/The-Apranik/Apranik/module/transport/base/udp"
	"github.com/The-Apranik/Apranik/spec"
)

type handled struct {
	network  spec.Network
	metadata spec.Metadata
}

// recordingHandler reports what core dispatched and holds nothing open.
type recordingHandler struct{ calls chan handled }

func (h recordingHandler) HandleStream(_ context.Context, c spec.Stream, m spec.Metadata) error {
	h.calls <- handled{spec.NetworkTCP, m}
	return c.Close()
}

func (h recordingHandler) HandleDatagram(_ context.Context, c spec.Datagram, m spec.Metadata) error {
	h.calls <- handled{spec.NetworkUDP, m}
	return c.Close()
}

func loopback(t *testing.T) spec.Endpoint {
	endpoint, err := spec.NewEndpoint(netip.MustParseAddr("127.0.0.1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

type mixedInboundOptions struct {
	socks5Settings   socks5.Settings
	httpSettings     proxyhttp.Settings
	firstByteTimeout time.Duration

	// Added to both protocols before the inbound listens, as the builder does.
	users []json.RawMessage
}

// startMixedInbound runs a real inbound on loopback TCP with the real socks5
// and http modules behind the mixed server, as the builder does.
func startMixedInbound(t *testing.T, options mixedInboundOptions) (net.Addr, recordingHandler) {
	t.Helper()
	address, handler, _ := startMixedInboundReturningIt(t, options)
	return address, handler
}

func startMixedInboundReturningIt(t *testing.T, options mixedInboundOptions) (net.Addr, recordingHandler, component.Inbound) {
	t.Helper()
	listener, err := tcp.NewBaseStreamTransportModule().NewStreamListener(loopback(t))
	if err != nil {
		t.Fatal(err)
	}
	socks5Server, err := (&socks5.Proxy{Settings: options.socks5Settings}).NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	httpServer, err := (&proxyhttp.Proxy{Settings: options.httpSettings}).NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}

	associateSocket := func(ctx context.Context) (spec.Datagram, error) {
		datagramListener, err := udp.NewBaseDatagramTransportModule().NewDatagramListener(loopback(t), dns.NewSystemResolver(nil))
		if err != nil {
			return nil, err
		}
		return datagramListener.ListenDatagram(ctx)
	}

	mixedServer := mixed.NewStreamServer(socks5Server, httpServer, options.firstByteTimeout)
	for _, user := range options.users {
		if _, err := mixedServer.(spec.UserManager).AddUser(user); err != nil {
			t.Fatal(err)
		}
	}

	in := inbound.NewStream(inbound.StreamConfig{
		Tag:             "mixed-in",
		Listener:        listener,
		ProxyServer:     mixedServer,
		AssociateSocket: associateSocket,
	})
	handler := recordingHandler{calls: make(chan handled, 16)}
	if err := in.Activate(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return listener.LocalAddr(), handler, in
}

func dialMixed(t *testing.T, address net.Addr) net.Conn {
	t.Helper()
	connection, err := net.Dial("tcp", address.String())
	if err != nil {
		t.Fatal(err)
	}
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	t.Cleanup(func() { connection.Close() })
	return connection
}

func readExactly(t *testing.T, reader io.Reader, size int) []byte {
	t.Helper()
	buffer := make([]byte, size)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatalf("reading %d bytes: %v", size, err)
	}
	return buffer
}

func awaitDispatch(t *testing.T, handler recordingHandler) handled {
	t.Helper()
	select {
	case got := <-handler.calls:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the handler")
		return handled{}
	}
}

func socks5ConnectRequest() []byte {
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len("example.com"))}
	request = append(request, "example.com"...)
	return append(request, 0x01, 0xBB)
}

func TestMixedServesSOCKS5Connect(t *testing.T) {
	address, handler := startMixedInbound(t, mixedInboundOptions{})
	connection := dialMixed(t, address)

	connection.Write([]byte{0x05, 0x01, 0x00})
	if method := readExactly(t, connection, 2); method[0] != 0x05 || method[1] != 0x00 {
		t.Fatalf("method reply = % X, want 05 00", method)
	}
	connection.Write(socks5ConnectRequest())
	if reply := readExactly(t, connection, 10); reply[1] != 0x00 {
		t.Fatalf("CONNECT reply = % X, want success", reply)
	}

	got := awaitDispatch(t, handler)
	if got.network != spec.NetworkTCP || got.metadata.Destination.String() != "example.com:443" || got.metadata.InboundTag != "mixed-in" {
		t.Errorf("dispatched as %+v", got)
	}
}

func TestMixedServesHTTPConnect(t *testing.T) {
	address, handler := startMixedInbound(t, mixedInboundOptions{})
	connection := dialMixed(t, address)

	connection.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))
	status, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil || !strings.Contains(status, " 200 ") {
		t.Fatalf("status line = %q, %v; want 200", status, err)
	}

	got := awaitDispatch(t, handler)
	if got.network != spec.NetworkTCP || got.metadata.Destination.String() != "example.com:443" {
		t.Errorf("dispatched as %+v", got)
	}
}

func TestMixedServesHTTPAbsoluteForm(t *testing.T) {
	address, handler := startMixedInbound(t, mixedInboundOptions{})
	connection := dialMixed(t, address)

	connection.Write([]byte("GET http://example.com/index.html HTTP/1.1\r\nHost: example.com\r\n\r\n"))

	got := awaitDispatch(t, handler)
	if got.network != spec.NetworkTCP || got.metadata.Destination.String() != "example.com:80" {
		t.Errorf("dispatched as %+v", got)
	}
}

// Core hands Associate its own carrier, not the mixed server's wrapper, so any
// byte the mixed server read ahead would corrupt the address read there. The
// greeting and request go in one write, as a pipelining client sends them, so
// a read-ahead has something to take.
func TestMixedServesSOCKS5UDPAssociate(t *testing.T) {
	address, handler := startMixedInbound(t, mixedInboundOptions{socks5Settings: socks5.Settings{UDPRelay: true}})
	connection := dialMixed(t, address)

	connection.Write([]byte{0x05, 0x01, 0x00, 0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	readExactly(t, connection, 2)
	reply := readExactly(t, connection, 10)
	if reply[1] != 0x00 {
		t.Fatalf("UDP ASSOCIATE reply = % X, want success", reply)
	}
	if boundPort := int(reply[8])<<8 | int(reply[9]); boundPort == 0 {
		t.Errorf("reply names no bound port: % X", reply)
	}

	got := awaitDispatch(t, handler)
	if got.network != spec.NetworkUDP || got.metadata.InboundTag != "mixed-in" {
		t.Errorf("dispatched as %+v", got)
	}
}

// mixed has one list of users, and both protocols ask for a login when it has any.
func TestMixedUsersApplyToBothProtocols(t *testing.T) {
	address, handler := startMixedInbound(t, mixedInboundOptions{users: []json.RawMessage{json.RawMessage(`{"name": "alice", "password": "secret"}`)}})

	socks5Connection := dialMixed(t, address)
	socks5Connection.Write([]byte{0x05, 0x01, 0x00})
	if method := readExactly(t, socks5Connection, 2); method[1] != 0xFF {
		t.Errorf("socks5 without credentials: method reply = % X, want 05 FF", method)
	}

	httpConnection := dialMixed(t, address)
	httpConnection.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))
	status, err := bufio.NewReader(httpConnection).ReadString('\n')
	if err != nil || !strings.Contains(status, " 407 ") {
		t.Errorf("http without credentials: status line = %q, %v; want 407", status, err)
	}

	select {
	case got := <-handler.calls:
		t.Errorf("a client without credentials was dispatched: %+v", got)
	default:
	}
}

func TestMixedClosesASilentClientAtTheFirstByteTimeout(t *testing.T) {
	address, handler := startMixedInbound(t, mixedInboundOptions{firstByteTimeout: 100 * time.Millisecond})
	connection := dialMixed(t, address)

	if _, err := connection.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("read on a silent connection = %v, want EOF once the inbound closes it", err)
	}
	select {
	case got := <-handler.calls:
		t.Errorf("a silent client was dispatched: %+v", got)
	default:
	}
}
