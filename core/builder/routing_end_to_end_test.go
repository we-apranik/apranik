package builder_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/core/route/dataset/geodata/geodatatest"
	"github.com/The-Apranik/Apranik/core/route/handler"
)

// startRoutedSOCKS5 runs a config file's single SOCKS5 inbound the way
// apranik run does: every outbound built, one router over them, and a handler
// per inbound. The config is loaded from a file beside its datasets, so
// relative dataset paths resolve as they would for a user.
func startRoutedSOCKS5(t *testing.T, directory, configText string) uint16 {
	t.Helper()
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	modules, err := registry.Default()
	if err != nil {
		t.Fatal(err)
	}
	coreBlueprint, diagnostics := config.LoadFile(configPath, modules)
	if diagnostics.HasErrors() {
		t.Fatalf("config refused: %v", diagnostics)
	}

	built, err := builder.New(coreBlueprint.Log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })

	datasets, err := built.LoadDatasets(coreBlueprint.Datasets)
	if err != nil {
		t.Fatal(err)
	}
	if err := built.UseDNS(coreBlueprint.DNS); err != nil {
		t.Fatal(err)
	}
	outbounds, err := built.BuildOutbounds(coreBlueprint.Outbounds)
	if err != nil {
		t.Fatal(err)
	}
	routing := builder.Routing{Outbounds: outbounds}
	t.Cleanup(func() { routing.Close() })
	routing.StartProbing(t.Context())
	if err := built.BuildDNS(coreBlueprint.DNS, outbounds, datasets); err != nil {
		t.Fatal(err)
	}
	trafficRouter, err := built.BuildRouter(coreBlueprint.Routing, outbounds, datasets)
	if err != nil {
		t.Fatal(err)
	}

	inbound, err := built.BuildInbound(coreBlueprint.Inbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inbound.Close() })
	if err := inbound.Activate(context.Background(), handler.New(trafficRouter, handler.Config{Resolver: built.Resolver()})); err != nil {
		t.Fatal(err)
	}
	return coreBlueprint.Inbounds[0].Bind.Port()
}

func startTCPEchoServer(t *testing.T) uint16 {
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
				io.Copy(connection, connection)
			}()
		}
	}()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

// socks5Connect opens a SOCKS5 CONNECT with no authentication to a host that
// is an IP or a domain, and returns the stream once the server accepted it.
func socks5Connect(t *testing.T, proxyPort uint16, host string, port uint16) net.Conn {
	t.Helper()
	connection, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	connection.SetDeadline(time.Now().Add(5 * time.Second))

	request := []byte{0x05, 0x01, 0x00, 0x05, 0x01, 0x00}
	if address, err := netip.ParseAddr(host); err == nil && address.Is4() {
		ipv4 := address.As4()
		request = append(append(request, 0x01), ipv4[:]...)
	} else {
		request = append(append(request, 0x03, byte(len(host))), host...)
	}
	request = binary.BigEndian.AppendUint16(request, port)
	if _, err := connection.Write(request); err != nil {
		t.Fatal(err)
	}

	reply := make([]byte, 2+10)
	if _, err := io.ReadFull(connection, reply); err != nil {
		t.Fatalf("SOCKS5 handshake: %v", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 || reply[3] != 0x00 {
		t.Fatalf("SOCKS5 refused: % x", reply)
	}
	return connection
}

// echoed reports whether payload came back whole, or the stream closed first.
func echoed(t *testing.T, connection net.Conn, payload string) bool {
	t.Helper()
	if _, err := connection.Write([]byte(payload)); err != nil {
		return false
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(connection, received); err != nil {
		return false
	}
	return bytes.Equal(received, []byte(payload))
}

func TestRoutingEndToEnd(t *testing.T) {
	directory := t.TempDir()
	site := geodatatest.Site(map[string][]geodatatest.Domain{
		"BLOCKED": {{Type: geodatatest.Suffix, Value: "blocked.test"}, {Type: geodatatest.Exact, Value: "localhost"}},
	})
	if err := os.WriteFile(filepath.Join(directory, "geosite.dat"), site, 0o600); err != nil {
		t.Fatal(err)
	}
	echoPort := startTCPEchoServer(t)
	blockedPort := startTCPEchoServer(t)
	proxyPort := freeLoopbackPort(t)

	proxy := startRoutedSOCKS5(t, directory, fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
		}],
		"outbounds": [
			{"tag": "proxy", "protocol": {"type": "freedom"}},
			{"tag": "direct", "protocol": {"type": "freedom"}}
		],
		"datasets": [
			{"tag": "blocked", "source": {"file": "geosite.dat"}, "format": {"type": "geodata", "kind": "domain", "category": "blocked"}}
		],
		"routing": {
			"rules": [
				{"match": {"port": [%d]}, "action": {"type": "block"}},
				{"match": {"inbound": ["socks-in"], "network": "tcp"},
				 "action": {"type": "sniff", "settings": {"sniffers": [{"type": "http"}], "timeout": "200ms"}}},
				{"match": {"domain_in_dataset": ["blocked"]}, "action": {"type": "block"}},
				{"match": {"ip_is_private": true}, "action": {"type": "route", "settings": {"outbound": "direct"}}}
			],
			"default_action": {"type": "route", "settings": {"outbound": "proxy"}}
		}
	}`, proxyPort, blockedPort))

	t.Run("allowed name is relayed whole after sniffing", func(t *testing.T) {
		connection := socks5Connect(t, proxy, "127.0.0.1", echoPort)
		if !echoed(t, connection, "GET / HTTP/1.1\r\nHost: allowed.test\r\n\r\n") {
			t.Fatal("an allowed request was not echoed")
		}
	})

	t.Run("sniffed name in a dataset is blocked", func(t *testing.T) {
		connection := socks5Connect(t, proxy, "127.0.0.1", echoPort)
		if echoed(t, connection, "GET / HTTP/1.1\r\nHost: www.blocked.test\r\n\r\n") {
			t.Fatal("a request for a blocked name got through")
		}
	})

	// localhost is in the dataset. The sniffed name wins when there is one;
	// without one, the destination's name decides.
	t.Run("sniffed name wins over the destination name", func(t *testing.T) {
		connection := socks5Connect(t, proxy, "localhost", echoPort)
		if !echoed(t, connection, "GET / HTTP/1.1\r\nHost: allowed.test\r\n\r\n") {
			t.Fatal("routed by the destination name although a name was sniffed")
		}
	})
	t.Run("destination name decides when nothing is sniffed", func(t *testing.T) {
		connection := socks5Connect(t, proxy, "localhost", echoPort)
		if echoed(t, connection, "not http\n") {
			t.Fatal("a blocked destination name got through")
		}
	})

	// The client sends nothing, as a server-first protocol's client would: a
	// sniff would wait out its 200ms before the block.
	t.Run("a rule before the sniff decides without waiting", func(t *testing.T) {
		connection := socks5Connect(t, proxy, "127.0.0.1", blockedPort)
		started := time.Now()
		if _, err := connection.Read(make([]byte, 1)); err == nil {
			t.Fatal("the blocked port was reached")
		}
		if waited := time.Since(started); waited > 150*time.Millisecond {
			t.Errorf("blocking took %s: the sniff ran before a rule that did not need it", waited)
		}
	})
}
