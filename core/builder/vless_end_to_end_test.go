package builder_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/builder"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/route/handler"
	"github.com/we-apranik/apranik/core/route/router"
	"github.com/we-apranik/apranik/spec"
)

const vlessAliceUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"

func startVLESSServer(t *testing.T, transport string) (port uint16, fallbackReceived <-chan []byte) {
	t.Helper()
	return startVLESSServerWith(t, transport, handler.Config{})
}

func startVLESSServerWith(t *testing.T, transport string, handlerConfig handler.Config) (port uint16, fallbackReceived <-chan []byte) {
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
			"tag": "vless-in", "listen": "127.0.0.1", "port": %d, "network": "tcp",
			"transport": %s,
			"protocol": { "type": "vless", "settings": { "users": [{ "name": "alice", "uuid": %q }], "handshake_timeout": "300ms" } },
			"fallback": { "server": "127.0.0.1", "server_port": %d }
		}],
		"outbounds": [{ "tag": "freedom", "protocol": { "type": "freedom" } }]
	}`, port, transport, vlessAliceUUID, fallback.Addr().(*net.TCPAddr).Port))

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
	if err := inbound.Activate(context.Background(), handler.New(router.NewWithDefaultOutbound(freedom), handlerConfig)); err != nil {
		t.Fatal(err)
	}
	return port, received
}

func buildVLESSClient(t *testing.T, transport string, serverPort uint16, uuid string) component.Outbound {
	t.Helper()
	return buildVLESSClientWith(t, transport, serverPort, uuid, "")
}

func buildVLESSClientWith(t *testing.T, transport string, serverPort uint16, uuid, extra string) component.Outbound {
	t.Helper()
	clientBlueprint := parseConfig(t, fmt.Sprintf(`{
		"version": 1,
		"outbounds": [{
			"tag": "vless-out", "server": "127.0.0.1", "server_port": %d,
			"transport": %s,
			"protocol": { "type": "vless", "settings": { "uuid": %q } }%s
		}]
	}`, serverPort, transport, uuid, extra))

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

func TestVLESSCarriesTCPAndUDPOverPlainTCP(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServer(t, stack)
	outbound := buildVLESSClient(t, stack, serverPort, vlessAliceUUID)
	checkTCPThrough(t, outbound)
	checkUDPThrough(t, outbound)
}

func TestVLESSOverWebSocketOverTLSCarriesTCPAndUDP(t *testing.T) {
	certificates := newTestCertificates(t)
	withWebSocket := func(tlsStack string) string {
		return tlsStack[:len(tlsStack)-1] + ", " + webSocketTransport(2048) + "]"
	}
	serverPort, _ := startVLESSServer(t, withWebSocket(tlsServerTransport(t, certificates)))
	outbound := buildVLESSClient(t, withWebSocket(tlsClientTransport(t, certificates)), serverPort, vlessAliceUUID)
	checkTCPThrough(t, outbound)
	checkUDPThrough(t, outbound)
}

// An ID written as short text is turned into a UUID on both ends the way Xray
// and sing-box do, so a client and a server given the same text agree.
func TestVLESSAcceptsAUserWrittenAsShortText(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	port := freeLoopbackPort(t)
	serverBlueprint := parseConfig(t, fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{ "tag": "in", "listen": "127.0.0.1", "port": %d, "network": "tcp", "transport": %s,
			"protocol": { "type": "vless", "settings": { "users": [{ "name": "alice", "uuid": "my-secret-id" }] } } }],
		"outbounds": [{ "tag": "freedom", "protocol": { "type": "freedom" } }]
	}`, port, stack))
	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })
	freedom, _ := built.BuildOutbound(serverBlueprint.Outbounds[0])
	t.Cleanup(func() { freedom.Close() })
	inbound, err := built.BuildInbound(serverBlueprint.Inbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inbound.Close() })
	if err := inbound.Activate(context.Background(), handler.New(router.NewWithDefaultOutbound(freedom), handler.Config{})); err != nil {
		t.Fatal(err)
	}

	checkTCPThrough(t, buildVLESSClient(t, stack, port, "my-secret-id"))
}

func TestVLESSCarriesOneUDPSessionToTwoDestinations(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServer(t, stack)
	checkOneUDPSessionToTwoDestinations(t, buildVLESSClient(t, stack, serverPort, vlessAliceUUID))
}

func countingForwarder(t *testing.T, targetPort uint16) (port uint16, connections *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	connections = new(atomic.Int32)
	go func() {
		for {
			accepted, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go func() {
				defer accepted.Close()
				target, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", targetPort))
				if err != nil {
					return
				}
				defer target.Close()
				go io.Copy(target, accepted)
				io.Copy(accepted, target)
			}()
		}
	}()
	return uint16(listener.Addr().(*net.TCPAddr).Port), connections
}

func TestVLESSXUDPUsesOneConnectionForAUDPSessionToTwoDestinations(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServer(t, stack)
	forwarderPort, connections := countingForwarder(t, serverPort)
	checkOneUDPSessionToTwoDestinations(t, buildVLESSClient(t, stack, forwarderPort, vlessAliceUUID))
	if got := connections.Load(); got != 1 {
		t.Errorf("the client made %d connections to the server, want 1", got)
	}
}

func TestVLESSPlainUDPUsesAConnectionPerDestination(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServer(t, stack)
	forwarderPort, connections := countingForwarder(t, serverPort)
	outbound := buildVLESSClientWith(t, stack, forwarderPort, vlessAliceUUID, `, "muxcool": { "udp": { "enabled": false } }`)
	checkOneUDPSessionToTwoDestinations(t, outbound)
	if got := connections.Load(); got != 2 {
		t.Errorf("the client made %d connections to the server, want 2", got)
	}
}

func checkOneUDPSessionToTwoDestinations(t *testing.T, outbound component.Outbound) {
	t.Helper()
	startEcho := func(prefix string) spec.Destination {
		echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { echo.Close() })
		go func() {
			buffer := make([]byte, 2048)
			for {
				n, from, err := echo.ReadFromUDP(buffer)
				if err != nil {
					return
				}
				echo.WriteToUDP(append([]byte(prefix), buffer[:n]...), from)
			}
		}()
		return loopbackDestination(t, echo.LocalAddr().(*net.UDPAddr).Port)
	}
	first, second := startEcho("first:"), startEcho("second:")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	datagram, err := outbound.CreateDatagram(ctx, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	defer datagram.Close()

	answers := make(chan [2]string, 4)
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, from, err := datagram.ReadFrom(buffer)
			if err != nil {
				return
			}
			answers <- [2]string{from.String(), string(buffer[:n])}
		}
	}()

	for _, step := range []struct {
		to      spec.Destination
		payload string
	}{{first, "a"}, {second, "b"}, {first, "c"}} {
		if _, err := datagram.WriteTo([]byte(step.payload), step.to); err != nil {
			t.Fatalf("WriteTo %s: %v", step.to, err)
		}
	}

	got := map[string][]string{}
	for range 3 {
		select {
		case answer := <-answers:
			got[answer[0]] = append(got[answer[0]], answer[1])
		case <-time.After(5 * time.Second):
			t.Fatalf("only %v arrived", got)
		}
	}
	if len(got[first.String()]) != 2 || len(got[second.String()]) != 1 || got[second.String()][0] != "second:b" {
		t.Errorf("answers = %v, want two from the first destination and second:b from the second", got)
	}
}

func TestVLESSServerHandsAWrongUUIDToTheFallback(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, fallbackReceived := startVLESSServer(t, stack)
	outbound := buildVLESSClient(t, stack, serverPort, "c0ffee00-0000-4000-8000-000000000000")

	stream, err := outbound.DialStream(context.Background(), spec.Metadata{Network: spec.NetworkTCP, Destination: loopbackDestination(t, 9)})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	stream.Write([]byte("hello"))

	select {
	case got := <-fallbackReceived:
		if !bytes.Contains(got, []byte("hello")) {
			t.Errorf("fallback received %q, want what the client sent, request included", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fallback received nothing")
	}
}

func startServerFirstBanner(t *testing.T) uint16 {
	t.Helper()
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
			go func() {
				defer connection.Close()
				connection.Write([]byte("220 hello\r\n"))
				io.Copy(io.Discard, connection)
			}()
		}
	}()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

func readBannerThroughVLESS(t *testing.T, extra string, within time.Duration) error {
	t.Helper()
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServer(t, stack)
	bannerPort := startServerFirstBanner(t)
	outbound := buildVLESSClientWith(t, stack, serverPort, vlessAliceUUID, fmt.Sprintf(extra, bannerPort))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := outbound.DialStream(ctx, spec.Metadata{Network: spec.NetworkTCP, Destination: loopbackDestination(t, int(bannerPort))})
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer stream.Close()
	stream.SetReadDeadline(time.Now().Add(within))
	banner := make([]byte, len("220 hello\r\n"))
	_, err = io.ReadFull(stream, banner)
	return err
}

func TestVLESSSendsTheRequestAtOnceToASkippedPortSoAServerFirstBannerArrives(t *testing.T) {
	err := readBannerThroughVLESS(t, `, "proxy_request_timing": {"max_delay": "1h", "skip": {"enabled": true, "ports": [%d]}}`, 3*time.Second)
	if err != nil {
		t.Fatalf("reading the banner: %v, want it to arrive without waiting for the delay", err)
	}
}

func TestVLESSHoldsTheRequestForAServerFirstPortThatIsNotSkipped(t *testing.T) {
	err := readBannerThroughVLESS(t, `, "proxy_request_timing": {"max_delay": "1h", "skip": {"enabled": false, "ports": [%d]}}`, 400*time.Millisecond)
	if !errors.Is(err, os.ErrDeadlineExceeded) && !errors.Is(err, io.EOF) {
		t.Fatalf("reading the banner: %v, want no banner because the request is still held", err)
	}
}

func killableForwarder(t *testing.T, targetPort uint16) (port uint16, cutAll func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	var mutex sync.Mutex
	var connections []net.Conn
	go func() {
		for {
			accepted, err := listener.Accept()
			if err != nil {
				return
			}
			target, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", targetPort))
			if err != nil {
				accepted.Close()
				continue
			}
			mutex.Lock()
			connections = append(connections, accepted, target)
			mutex.Unlock()
			go func() { io.Copy(target, accepted); target.Close() }()
			go func() { io.Copy(accepted, target); accepted.Close() }()
		}
	}()
	return uint16(listener.Addr().(*net.TCPAddr).Port), func() {
		mutex.Lock()
		defer mutex.Unlock()
		for _, connection := range connections {
			connection.Close()
		}
		connections = nil
	}
}

func startPortEcho(t *testing.T) spec.Destination {
	t.Helper()
	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	go func() {
		buffer := make([]byte, 2048)
		for {
			_, from, err := echo.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			echo.WriteToUDP([]byte(fmt.Sprint(from.Port)), from)
		}
	}()
	return loopbackDestination(t, echo.LocalAddr().(*net.UDPAddr).Port)
}

func sessionFrom(t *testing.T, address string) spec.Metadata {
	t.Helper()
	endpoint, err := spec.NewEndpoint(netip.MustParseAddrPort(address).Addr(), netip.MustParseAddrPort(address).Port())
	if err != nil {
		t.Fatal(err)
	}
	return spec.Metadata{Network: spec.NetworkUDP, User: "alice", Transport: spec.TransportMetadata{SourceEndpoint: endpoint}}
}

func sourcePortSeenThroughANewStream(t *testing.T, outbound component.Outbound, session spec.Metadata, destination spec.Destination) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	datagram, err := outbound.CreateDatagram(ctx, session)
	if err != nil {
		t.Fatalf("CreateDatagram: %v", err)
	}
	t.Cleanup(func() { datagram.Close() })
	if _, err := datagram.WriteTo([]byte("hello"), destination); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	answer := make(chan string, 1)
	go func() {
		buffer := make([]byte, 100)
		n, _, err := datagram.ReadFrom(buffer)
		if err != nil {
			answer <- "error: " + err.Error()
			return
		}
		answer <- string(buffer[:n])
	}()
	select {
	case got := <-answer:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no answer came back")
		return ""
	}
}

func TestVLESSXUDPResumesASessionAfterItsStreamIsCut(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServerWith(t, stack, handler.Config{ResumeUDPSessions: true, UDPSessionResumeHoldTime: time.Minute})
	forwarderPort, cutAll := killableForwarder(t, serverPort)
	outbound := buildVLESSClient(t, stack, forwarderPort, vlessAliceUUID)
	destination := startPortEcho(t)
	session := sessionFrom(t, "203.0.113.7:50000")

	before := sourcePortSeenThroughANewStream(t, outbound, session, destination)
	cutAll()
	time.Sleep(100 * time.Millisecond)
	after := sourcePortSeenThroughANewStream(t, outbound, session, destination)
	if before == "" || before != after {
		t.Errorf("the destination saw source port %q and then %q, want the same one", before, after)
	}

	another := sourcePortSeenThroughANewStream(t, outbound, sessionFrom(t, "203.0.113.7:50001"), destination)
	if another == after {
		t.Errorf("another session was given the same source port %q", another)
	}
}

func TestVLESSXUDPDoesNotResumeWhenTheServerHasItOff(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServerWith(t, stack, handler.Config{})
	forwarderPort, cutAll := killableForwarder(t, serverPort)
	outbound := buildVLESSClient(t, stack, forwarderPort, vlessAliceUUID)
	destination := startPortEcho(t)
	session := sessionFrom(t, "203.0.113.7:50000")

	before := sourcePortSeenThroughANewStream(t, outbound, session, destination)
	cutAll()
	time.Sleep(100 * time.Millisecond)
	after := sourcePortSeenThroughANewStream(t, outbound, session, destination)
	if before == after {
		t.Errorf("the destination saw source port %q twice, want a new session to get a new port", before)
	}
}

func TestVLESSXUDPClientWithTheGlobalIDOffGetsANewSessionEachTime(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServerWith(t, stack, handler.Config{ResumeUDPSessions: true, UDPSessionResumeHoldTime: time.Minute})
	forwarderPort, cutAll := killableForwarder(t, serverPort)
	outbound := buildVLESSClientWith(t, stack, forwarderPort, vlessAliceUUID, `, "muxcool": { "udp": { "xudp": { "global_id": { "enabled": false } } } }`)
	destination := startPortEcho(t)
	session := sessionFrom(t, "203.0.113.7:50000")

	before := sourcePortSeenThroughANewStream(t, outbound, session, destination)
	cutAll()
	time.Sleep(100 * time.Millisecond)
	after := sourcePortSeenThroughANewStream(t, outbound, session, destination)
	if before == after {
		t.Errorf("the destination saw source port %q twice, want a new session to get a new port", before)
	}
}
