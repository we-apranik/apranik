package builder_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/spec"
)

func buildTrojanClientWithPassword(t *testing.T, tag, transport string, serverPort uint16, password string) component.Outbound {
	t.Helper()
	clientBlueprint := parseConfig(t, fmt.Sprintf(`{
		"version": 1,
		"outbounds": [{
			"tag": %q, "server": "127.0.0.1", "server_port": %d,
			"transport": %s,
			"protocol": { "type": "trojan", "settings": { "password": %q } }
		}]
	}`, tag, serverPort, transport, password))

	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { built.Close() })
	member, err := built.BuildOutbound(clientBlueprint.Outbounds[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { member.Close() })
	return member
}

// echoesThrough reports whether a message came back whole; unlike
// checkTCPThrough it fails quietly, so a test can wait for it to start working.
func echoesThrough(member component.Outbound, echoPort uint16) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	destination, err := spec.NewDestinationFromIP(loopbackAddress, echoPort)
	if err != nil {
		return false
	}
	stream, err := member.DialStream(ctx, spec.Metadata{Network: spec.NetworkTCP, Destination: destination})
	if err != nil {
		return false
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(time.Second))
	if _, err := stream.Write([]byte("through a trojan member")); err != nil {
		return false
	}
	reply := make([]byte, len("through a trojan member"))
	_, err = io.ReadFull(stream, reply)
	return err == nil && string(reply) == "through a trojan member"
}

// Trojan sends its request with the first write, so dialing a member with the
// wrong password succeeds and only the first exchange fails. Only a probe, which
// sends a real request through the member, can tell the two members apart.
func TestAGroupProbesThroughTrojanAndLeavesAMemberWithTheWrongPassword(t *testing.T) {
	certificates := newTestCertificates(t)
	serverPort, _ := startServer(t, certificates)
	wrongPassword := buildTrojanClientWithPassword(t, "wrong-password", tlsClientTransport(t, certificates), serverPort, "not the password")
	rightPassword := buildTrojanClientWithPassword(t, "right-password", tlsClientTransport(t, certificates), serverPort, "correct horse")
	echoPort := startTCPEchoServer(t)

	probeTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(probeTarget.Close)

	for _, groupType := range []string{"fallback", "urltest"} {
		t.Run(groupType, func(t *testing.T) {
			group, err := outbound.NewGroup(outbound.GroupConfig{
				Tag:          "group",
				Members:      []component.Outbound{wrongPassword, rightPassword},
				PicksFastest: groupType == "urltest",
				Probe:        outbound.ProbeConfig{URL: probeTarget.URL + "/", Interval: 50 * time.Millisecond, Timeout: time.Second},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { group.Close() })
			group.StartProbing(t.Context())

			if !eventuallyTrue(20*time.Second, func() bool { return echoesThrough(group, echoPort) }) {
				t.Fatal("the group never moved to the member with the right password")
			}
		})
	}
}

func eventuallyTrue(limit time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// A real SOCKS5 proxy is a member: the group's connection goes through the
// upstream proxy the member names.
func TestAGroupMemberMayBeARealSOCKS5Proxy(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	upstreamPort := startRoutedSOCKS5(t, t.TempDir(), fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{"tag": "upstream-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}],
		"outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}]
	}`, freeLoopbackPort(t)))
	probeTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(probeTarget.Close)

	proxyPort := freeLoopbackPort(t)
	proxy := startRoutedSOCKS5(t, t.TempDir(), fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
		}],
		"outbounds": [
			{"tag": "only-upstream", "protocol": {"type": "urltest", "settings": {
				"outbounds": ["upstream"],
				"probe": {"type": "http", "url": %q, "interval": "50ms", "timeout": "2s"}
			}}},
			{"tag": "upstream", "server": "127.0.0.1", "server_port": %d,
			 "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}
		]
	}`, proxyPort, probeTarget.URL+"/", upstreamPort))

	connection := socks5Connect(t, proxy, "127.0.0.1", echoPort)
	if !echoed(t, connection, "through a socks5 member") {
		t.Fatal("the connection was not relayed through the SOCKS5 member")
	}
}

var loopbackAddress = netip.MustParseAddr("127.0.0.1")
