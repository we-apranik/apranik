package builder_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestAGroupRoutesPastAMemberThatFails(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	probeTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(probeTarget.Close)

	for _, groupType := range []string{"urltest", "fallback"} {
		t.Run(groupType, func(t *testing.T) {
			proxyPort := freeLoopbackPort(t)
			deadServerPort := freeLoopbackPort(t)
			proxy := startRoutedSOCKS5(t, t.TempDir(), fmt.Sprintf(`{
				"version": 1,
				"inbounds": [{
					"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
					"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
				}],
				"outbounds": [
					{"tag": "auto", "protocol": {"type": %q, "settings": {
						"members": ["dead", "direct"],
						"probe": {"url": %q, "interval": "50ms", "timeout": "1s"}
					}}},
					{"tag": "dead", "server": "127.0.0.1", "server_port": %d,
					 "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}},
					{"tag": "direct", "protocol": {"type": "freedom"}}
				],
				"routing": {"default_action": {"type": "route", "settings": {"outbound": "auto"}}}
			}`, proxyPort, groupType, probeTarget.URL+"/", deadServerPort))

			connection := socks5Connect(t, proxy, "127.0.0.1", echoPort)
			if !echoed(t, connection, "through a "+groupType+" group") {
				t.Fatal("the connection was not relayed through the group's working member")
			}
		})
	}
}

func TestADNSServerMayGoThroughAGroup(t *testing.T) {
	zone := &testZone{addresses: map[string]netip.Addr{"app.test.": netip.MustParseAddr("127.0.0.1")}}
	dnsServer := startUDPDNSServer(t, zone)
	echoPort := startTCPEchoServer(t)
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
			{"tag": "direct", "protocol": {"type": "freedom"}},
			{"tag": "auto", "protocol": {"type": "fallback", "settings": {
				"members": ["direct"],
				"probe": {"url": %q, "interval": "1h"}
			}}}
		],
		"dns": {"servers": [{"tag": "s", "type": "udp", "settings": {"server": %q}, "outbound": "auto"}],
			"default_server": "s", "ip_versions": "ipv4_only"}
	}`, proxyPort, probeTarget.URL+"/", dnsServer))

	before := zone.queries.Load()
	connection := socks5Connect(t, proxy, "app.test", echoPort)
	if !echoed(t, connection, "resolved through a group") {
		t.Fatal("the connection to a name resolved through a group was not relayed")
	}
	if zone.queries.Load() == before {
		t.Fatal("the DNS server behind the group was never asked")
	}
}
