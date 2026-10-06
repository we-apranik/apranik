package builder_test

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/stats"
)

func startUDPEchoServer(t *testing.T) uint16 {
	t.Helper()
	echo, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	go func() {
		buffer := make([]byte, 2048)
		for {
			size, from, err := echo.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			echo.WriteToUDP(buffer[:size], from)
		}
	}()
	return uint16(echo.LocalAddr().(*net.UDPAddr).Port)
}

// socks5Associate opens a UDP ASSOCIATE with no authentication and returns a
// UDP socket connected to the relay address, plus the control stream, which
// must stay open for the association to live.
func socks5Associate(t *testing.T, proxyPort uint16) (relaySide *net.UDPConn, control net.Conn) {
	t.Helper()
	control, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { control.Close() })
	control.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := control.Write([]byte{0x05, 0x01, 0x00, 0x05, 0x03, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2+10)
	if _, err := io.ReadFull(control, reply); err != nil {
		t.Fatalf("SOCKS5 UDP ASSOCIATE: %v", err)
	}
	if reply[1] != 0x00 || reply[3] != 0x00 {
		t.Fatalf("SOCKS5 UDP ASSOCIATE refused: %v", reply)
	}
	relayPort := binary.BigEndian.Uint16(reply[2+8:])
	relaySide, err = net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(relayPort)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { relaySide.Close() })
	return relaySide, control
}

// Sends one datagram to 127.0.0.1:port in SOCKS5's UDP framing and waits for
// the echo, returning its payload.
func socks5UDPExchange(t *testing.T, relaySide *net.UDPConn, port uint16, payload string) string {
	t.Helper()
	frame := append([]byte{0, 0, 0, 0x01, 127, 0, 0, 1}, binary.BigEndian.AppendUint16(nil, port)...)
	if _, err := relaySide.Write(append(frame, payload...)); err != nil {
		t.Fatal(err)
	}
	relaySide.SetReadDeadline(time.Now().Add(5 * time.Second))
	buffer := make([]byte, 2048)
	size, err := relaySide.Read(buffer)
	if err != nil {
		t.Fatalf("no UDP reply: %v", err)
	}
	return string(buffer[10:size])
}

func TestUDPThroughSOCKS5IsCountedByNetworkAndListedPerDestination(t *testing.T) {
	firstPort, secondPort := startUDPEchoServer(t), startUDPEchoServer(t)
	port, registry := startCountedSOCKS5(t, t.TempDir(),
		statsConfig(freeLoopbackPort(t), startProbeTarget(t), `, "stats": {"live_connections": true}`))
	relaySide, _ := socks5Associate(t, port)

	for _, payload := range []string{"one", "three", "seven.."} {
		if got := socks5UDPExchange(t, relaySide, firstPort, payload); got != payload {
			t.Fatalf("echo = %q, want %q", got, payload)
		}
	}
	if got := socks5UDPExchange(t, relaySide, secondPort, "other"); got != "other" {
		t.Fatalf("echo = %q, want other", got)
	}

	const wantBytes = 3 + 5 + 7 + 5
	snapshot := eventuallySnapshot(t, registry, "the datagrams counted under the inbound",
		func(snapshot trafficView) bool {
			return snapshot.Inbounds["socks-in"].UDP.DownlinkPackets == 4
		})
	want := stats.UDPTraffic{UplinkBytes: wantBytes, DownlinkBytes: wantBytes, UplinkPackets: 4, DownlinkPackets: 4, Sessions: 1}
	for name, traffic := range map[string]stats.Traffic{
		"inbound socks-in": snapshot.Inbounds["socks-in"].Traffic,
		"group auto":       snapshot.Outbounds["auto"].Traffic,
		"member direct":    snapshot.Outbounds["direct"].Traffic,
	} {
		if traffic.UDP != want {
			t.Errorf("%s UDP = %+v, want %+v", name, traffic.UDP, want)
		}
		if traffic.TCP.TotalConnections > 1 {
			t.Errorf("%s TCP = %+v: a UDP datagram is not a TCP connection", name, traffic.TCP)
		}
	}
	if snapshot.Outbounds["unused"].UDP != (stats.UDPTraffic{}) {
		t.Error("an outbound that carried nothing counted UDP")
	}
	totals := registry.TotalTraffic()
	if totals.UDP.UplinkBytes != wantBytes || totals.UDP.DownlinkBytes != wantBytes || totals.All.UplinkBytes != wantBytes {
		t.Errorf("totals = %+v", totals)
	}

	live := registry.LiveConnections()
	if len(live) != 2 {
		t.Fatalf("%d live entries, want one per destination", len(live))
	}
	for _, entry := range live {
		if entry.Info.Network != "udp" || len(entry.Info.OutboundTags) != 2 || entry.Info.OutboundTags[0] != "auto" || entry.Info.OutboundTags[1] != "direct" {
			t.Errorf("entry = %+v, want a UDP entry through auto then direct", entry.Info)
		}
	}
	if hits := registry.RuleHits()["default"]; hits != 2 {
		t.Errorf("default rule hits = %d, want 2: one per routed destination", hits)
	}
}

// Replies a server makes as large as asked: "big" gets 9000 bytes, anything else is echoed.
func startUDPBigReplyServer(t *testing.T) uint16 {
	t.Helper()
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	go func() {
		buffer := make([]byte, 2048)
		for {
			size, from, err := server.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if string(buffer[:size]) == "big" {
				server.WriteToUDP(make([]byte, 9000), from)
				continue
			}
			server.WriteToUDP(buffer[:size], from)
		}
	}()
	return uint16(server.LocalAddr().(*net.UDPAddr).Port)
}

func TestADatagramOverTheLimitIsDroppedAndCountedAndTheSessionGoesOn(t *testing.T) {
	serverPort := startUDPBigReplyServer(t)
	port, registry := startCountedSOCKS5(t, t.TempDir(), statsConfig(freeLoopbackPort(t), startProbeTarget(t), ""))
	relaySide, _ := socks5Associate(t, port)

	// Too big from the client: 9000 bytes against the default 8192.
	frame := append([]byte{0, 0, 0, 0x01, 127, 0, 0, 1}, binary.BigEndian.AppendUint16(nil, serverPort)...)
	if _, err := relaySide.Write(append(frame, make([]byte, 9000)...)); err != nil {
		t.Fatal(err)
	}
	eventuallySnapshot(t, registry, "the oversized datagram from the client counted as a drop",
		func(snapshot trafficView) bool {
			return snapshot.Inbounds["socks-in"].DatagramDropsByReason["too_large_from_client"] == 1
		})

	// Too big from the server: a 9000 byte reply to a small request.
	frame = append([]byte{0, 0, 0, 0x01, 127, 0, 0, 1}, binary.BigEndian.AppendUint16(nil, serverPort)...)
	if _, err := relaySide.Write(append(frame, "big"...)); err != nil {
		t.Fatal(err)
	}
	eventuallySnapshot(t, registry, "the oversized reply counted as a drop on the outbound",
		func(snapshot trafficView) bool {
			return snapshot.Outbounds["direct"].DatagramDropsByReason["too_large_from_outbound"] == 1
		})

	// The same session still works.
	if got := socks5UDPExchange(t, relaySide, serverPort, "still here"); got != "still here" {
		t.Errorf("echo after the oversized datagrams = %q, want the session to go on", got)
	}
}
