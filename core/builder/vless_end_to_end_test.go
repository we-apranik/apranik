package builder_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/route/handler"
	"github.com/The-Apranik/Apranik/core/route/router"
	"github.com/The-Apranik/Apranik/spec"
)

// VLESS server and client built from config files, as `apranik run` builds
// them, over a given transport stack.

const vlessAliceUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"

func startVLESSServer(t *testing.T, transport string) (port uint16, fallbackReceived <-chan []byte) {
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
	if err := inbound.Activate(context.Background(), handler.New(router.NewWithDefaultOutbound(freedom), handler.Config{})); err != nil {
		t.Fatal(err)
	}
	return port, received
}

func buildVLESSClient(t *testing.T, transport string, serverPort uint16, uuid string) component.Outbound {
	t.Helper()
	return buildVLESSClientWith(t, transport, serverPort, uuid, "")
}

// extra is more members of the outbound, such as `, "muxcool": {...}`.
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

// One UDP session talking to two destinations; both answers come back through
// the one datagram from the right destination. With no muxcool key the client
// sends XUDP, the other cores' default.
func TestVLESSCarriesOneUDPSessionToTwoDestinations(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServer(t, stack)
	checkOneUDPSessionToTwoDestinations(t, buildVLESSClient(t, stack, serverPort, vlessAliceUUID))
}

// countingForwarder passes every connection to targetPort and counts them, so a
// test can tell how many connections a client made to the server.
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

// XUDP carries a whole UDP session on one connection to the server, whatever
// the number of destinations.
func TestVLESSXUDPUsesOneConnectionForAUDPSessionToTwoDestinations(t *testing.T) {
	stack := `[{ "type": "tcp" }]`
	serverPort, _ := startVLESSServer(t, stack)
	forwarderPort, connections := countingForwarder(t, serverPort)
	checkOneUDPSessionToTwoDestinations(t, buildVLESSClient(t, stack, forwarderPort, vlessAliceUUID))
	if got := connections.Load(); got != 1 {
		t.Errorf("the client made %d connections to the server, want 1", got)
	}
}

// With the UDP part switched off the protocol's own UDP is used: plain VLESS
// names one destination per stream, so a stream is opened for each.
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
