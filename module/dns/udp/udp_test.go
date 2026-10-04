package udp_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/module/dns/udp"
	"github.com/The-Apranik/Apranik/spec"
)

// socketDatagram is a spec.Datagram over a plain UDP socket, so this test
// needs no transport module.
type socketDatagram struct{ connection *net.UDPConn }

func (d socketDatagram) ReadFrom(buffer []byte) (int, spec.Destination, error) {
	large := make([]byte, 65535)
	length, from, err := d.connection.ReadFromUDPAddrPort(large)
	if err != nil {
		return 0, spec.Destination{}, err
	}
	destination, _ := spec.NewDestinationFromIP(from.Addr().Unmap(), from.Port())
	if length > len(buffer) {
		return 0, destination, spec.ErrMessageTooLong
	}
	return copy(buffer, large[:length]), destination, nil
}

func (d socketDatagram) WriteTo(payload []byte, to spec.Destination) (int, error) {
	ip, _ := to.IP()
	return d.connection.WriteToUDPAddrPort(payload, netip.AddrPortFrom(ip, to.Port()))
}

func (d socketDatagram) Close() error        { return d.connection.Close() }
func (d socketDatagram) LocalAddr() net.Addr { return d.connection.LocalAddr() }

type socketDialer struct{}

func (socketDialer) CreateDatagram(context.Context, spec.Endpoint) (spec.Datagram, error) {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	return socketDatagram{connection}, nil
}

func (socketDialer) Close() error { return nil }

func asResponse(query []byte) []byte {
	response := append([]byte(nil), query...)
	response[2] |= 0x80
	return response
}

// startServer answers each query with whatever respond returns, packet by
// packet; nil answers nothing.
func startServer(t *testing.T, respond func(query []byte) [][]byte) *net.UDPConn {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	go func() {
		buffer := make([]byte, 65535)
		for {
			length, from, err := connection.ReadFromUDPAddrPort(buffer)
			if err != nil {
				return
			}
			for _, packet := range respond(append([]byte(nil), buffer[:length]...)) {
				connection.WriteToUDPAddrPort(packet, from)
			}
		}
	}()
	return connection
}

func newServer(t *testing.T, address string, timeout string) spec.DNSServer {
	t.Helper()
	module := udp.NewDatagramDNSServerModule()
	settings := `{"server": "` + address + `"`
	if timeout != "" {
		settings += `, "timeout": "` + timeout + `"`
	}
	if err := json.Unmarshal([]byte(settings+"}"), module); err != nil {
		t.Fatal(err)
	}
	if err := module.Validate(); err != nil {
		t.Fatal(err)
	}
	server, err := module.NewDatagramDNSServer(socketDialer{})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func query(id uint16) []byte {
	message := make([]byte, 20)
	binary.BigEndian.PutUint16(message, id)
	return message
}

func TestQueryReturnsTheServersAnswer(t *testing.T) {
	listener := startServer(t, func(q []byte) [][]byte { return [][]byte{asResponse(q)} })
	server := newServer(t, listener.LocalAddr().String(), "")
	response, err := server.Query(context.Background(), query(0xABCD))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(response) != 0xABCD || response[2]&0x80 == 0 {
		t.Fatalf("response %x", response)
	}
}

// A wrong ID and a non-response are what blind injection looks like; both
// are skipped and the real answer still taken.
func TestQueryIgnoresWhatDoesNotAnswerIt(t *testing.T) {
	listener := startServer(t, func(q []byte) [][]byte {
		wrongID := asResponse(q)
		wrongID[0] ^= 0xFF
		notAResponse := append([]byte(nil), q...)
		real := asResponse(q)
		real[19] = 0x42
		return [][]byte{wrongID, notAResponse, {1, 2, 3}, real}
	})
	server := newServer(t, listener.LocalAddr().String(), "")
	response, err := server.Query(context.Background(), query(7))
	if err != nil {
		t.Fatal(err)
	}
	if response[19] != 0x42 {
		t.Fatalf("took the wrong packet: %x", response)
	}
}

func TestQueryIgnoresAnswersFromAnotherAddress(t *testing.T) {
	listener := startServer(t, func([]byte) [][]byte { return nil })
	impostor, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	defer impostor.Close()

	// The impostor learns the client's port from the server and answers first.
	server := newServer(t, listener.LocalAddr().String(), "300ms")
	go func() {
		buffer := make([]byte, 512)
		listener.SetReadDeadline(time.Now().Add(2 * time.Second))
		length, from, err := listener.ReadFromUDPAddrPort(buffer)
		if err == nil {
			impostor.WriteToUDPAddrPort(asResponse(buffer[:length]), from)
		}
	}()
	if _, err := server.Query(context.Background(), query(9)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a timeout: the answer came from the wrong address", err)
	}
}

func TestQueryTimesOut(t *testing.T) {
	listener := startServer(t, func([]byte) [][]byte { return nil })
	server := newServer(t, listener.LocalAddr().String(), "50ms")
	started := time.Now()
	if _, err := server.Query(context.Background(), query(1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the timeout did not bound the query")
	}
}

func TestAnOversizedAnswerIsTruncated(t *testing.T) {
	listener := startServer(t, func(q []byte) [][]byte {
		return [][]byte{append(asResponse(q), make([]byte, 5000)...)}
	})
	server := newServer(t, listener.LocalAddr().String(), "")
	if _, err := server.Query(context.Background(), query(1)); !errors.Is(err, spec.ErrDNSAnswerTruncated) {
		t.Fatalf("err = %v", err)
	}
}

func TestSettings(t *testing.T) {
	module := udp.NewDatagramDNSServerModule()
	if err := module.Validate(); err == nil {
		t.Error("no server was accepted")
	}
	json.Unmarshal([]byte(`{"server": "8.8.8.8", "timeout": "-1s"}`), module)
	if err := module.Validate(); err == nil {
		t.Error("a negative timeout was accepted")
	}
	hostname := udp.NewDatagramDNSServerModule()
	json.Unmarshal([]byte(`{"server": "dns.example"}`), hostname)
	if len(hostname.Warnings()) == 0 {
		t.Error("a hostname server raised no warning")
	}
}
