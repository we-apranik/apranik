package builder_test

import (
	"time"

	"fmt"
	"testing"

	"github.com/we-apranik/apranik/core/apicoreprovider"
	"github.com/we-apranik/apranik/core/component"
)

func TestASelectorSendsNewConnectionsToTheMemberThatWasChosen(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	proxyPort := freeLoopbackPort(t)
	deadServerPort := freeLoopbackPort(t)
	port, _, outbounds, _ := startSOCKS5WithOutbounds(t, t.TempDir(), fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
		}],
		"outbounds": [
			{"tag": "proxy", "protocol": {"type": "selector", "settings": {"members": ["direct", "dead"]}}},
			{"tag": "direct", "protocol": {"type": "freedom"}},
			{"tag": "dead", "server": "127.0.0.1", "server_port": %d,
			 "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}
		],
		"routing": {"default_action": {"type": "route", "settings": {"outbound": "proxy"}}}
	}`, proxyPort, deadServerPort))

	var selector component.SelectableOutbound
	for _, built := range outbounds {
		if built.Tag() == "proxy" {
			selector = built.(component.SelectableOutbound)
		}
	}
	if selector == nil {
		t.Fatal("the selector was not built as a SelectableOutbound")
	}

	if !echoed(t, socks5Connect(t, port, "127.0.0.1", echoPort), "on the first member") {
		t.Fatal("a connection did not go through the selector's first member")
	}

	if _, err := selector.SelectMember("dead"); err != nil {
		t.Fatal(err)
	}
	if echoed(t, socks5Connect(t, port, "127.0.0.1", echoPort), "on the dead member") {
		t.Fatal("a new connection got through the dead member the person chose")
	}

	if _, err := selector.SelectMember("direct"); err != nil {
		t.Fatal(err)
	}
	if !echoed(t, socks5Connect(t, port, "127.0.0.1", echoPort), "back on the first member") {
		t.Fatal("a connection did not go through the member chosen again")
	}
}

func TestASwitchThroughTheProviderEndsTheOpenConnectionThroughTheOldMember(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	proxyPort := freeLoopbackPort(t)
	deadServerPort := freeLoopbackPort(t)
	port, registry, outbounds, described := startSOCKS5WithOutbounds(t, t.TempDir(), fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
		}],
		"outbounds": [
			{"tag": "proxy", "protocol": {"type": "selector", "settings": {"members": ["direct", "dead"]}}},
			{"tag": "direct", "protocol": {"type": "freedom"}},
			{"tag": "dead", "server": "127.0.0.1", "server_port": %d,
			 "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}
		],
		"routing": {"default_action": {"type": "route", "settings": {"outbound": "proxy"}}},
		"stats": {"tracking": {"enabled": true}}
	}`, proxyPort, deadServerPort))
	withoutInbounds := *described
	withoutInbounds.Inbounds = nil
	provider, err := apicoreprovider.New(apicoreprovider.RunningCore{Stats: registry, Blueprint: &withoutInbounds, Outbounds: outbounds})
	if err != nil {
		t.Fatal(err)
	}

	open := socks5Connect(t, port, "127.0.0.1", echoPort)
	if !echoed(t, open, "before the switch") {
		t.Fatal("the connection through the first member did not work")
	}
	live, err := provider.LiveConversations()
	if err != nil || len(live) != 1 || len(live[0].OutboundTags) != 2 || live[0].OutboundTags[1] != "direct" {
		t.Fatalf("live conversations = %+v, %v; want one through proxy then direct", live, err)
	}

	if err := provider.SelectMember("proxy", "dead"); err != nil {
		t.Fatal(err)
	}

	open.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := open.Read(make([]byte, 1)); err == nil {
		t.Error("the open connection survived the switch away from its member")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(registry.LiveConversations()) != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if left := registry.LiveConversations(); len(left) != 0 {
		t.Errorf("%d connections still listed after the switch", len(left))
	}
}
