package builder_test

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config"
	"github.com/we-apranik/apranik/core/builder"
	"github.com/we-apranik/apranik/core/registry"
	"github.com/we-apranik/apranik/core/stats"
)

func eventuallySnapshot(t *testing.T, registry *stats.Registry, description string, condition func(trafficView) bool) trafficView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot := viewOf(registry); condition(snapshot) {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never held: %+v", description, viewOf(registry))
	return trafficView{}
}

func statsConfig(inboundPort uint16, probeURL, extraSections string) string {
	return fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{
			"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}
		}],
		"outbounds": [
			{"tag": "auto", "protocol": {"type": "fallback", "settings": {
				"members": ["direct"],
				"probe": {"url": %q, "interval": "1h"}
			}}},
			{"tag": "direct", "protocol": {"type": "freedom"}},
			{"tag": "unused", "protocol": {"type": "freedom"}}
		],
		"routing": {"default_action": {"type": "route", "settings": {"outbound": "auto"}}}%s
	}`, inboundPort, probeURL, extraSections)
}

func startProbeTarget(t *testing.T) string {
	t.Helper()
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	return target.URL + "/"
}

func TestTrafficThroughAGroupIsCountedUnderInboundGroupAndMember(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	port, registry := startCountedSOCKS5(t, t.TempDir(), statsConfig(freeLoopbackPort(t), startProbeTarget(t), ""))

	connection := socks5Connect(t, port, "127.0.0.1", echoPort)
	const payload = "counted through a group"
	if !echoed(t, connection, payload) {
		t.Fatal("the connection was not relayed")
	}

	snapshot := eventuallySnapshot(t, registry, "payload counted under the inbound",
		func(snapshot trafficView) bool {
			return snapshot.Inbounds["socks-in"].DownlinkBytes == uint64(len(payload))
		})
	for name, traffic := range map[string]stats.Traffic{
		"inbound socks-in": snapshot.Inbounds["socks-in"].Traffic,
		"group auto":       snapshot.Outbounds["auto"].Traffic,
		"member direct":    snapshot.Outbounds["direct"].Traffic,
	} {
		if traffic.UplinkBytes != uint64(len(payload)) || traffic.DownlinkBytes != uint64(len(payload)) {
			t.Errorf("%s: uplink %d, downlink %d, want %d each", name, traffic.UplinkBytes, traffic.DownlinkBytes, len(payload))
		}
		if traffic.TCP.TotalConnections != 1 || traffic.TCP.ActiveConnections != 1 {
			t.Errorf("%s: %d total, %d active connections while open, want 1 and 1", name, traffic.TCP.TotalConnections, traffic.TCP.ActiveConnections)
		}
	}
	if !snapshot.Outbounds["auto"].IsGroup || snapshot.Outbounds["direct"].IsGroup {
		t.Error("the snapshot does not mark which outbound is a group")
	}
	if snapshot.Outbounds["unused"].TCP.TotalConnections != 0 {
		t.Error("an outbound that carried nothing was counted")
	}

	connection.Close()
	eventuallySnapshot(t, registry, "no active connections after the client left",
		func(snapshot trafficView) bool { return snapshot.Inbounds["socks-in"].TCP.ActiveConnections == 0 })
}

func TestRuleHitsAreCountedPerRuleLocationThroughTheBuilder(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	config := fmt.Sprintf(`{
		"version": 1,
		"inbounds": [{"tag": "socks-in", "listen": "127.0.0.1", "port": %d,
			"transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}],
		"outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}],
		"routing": {
			"rules": [
				{"match": {"port": [1]}, "action": {"type": "block"}},
				{"match": {"port": [%d]}, "action": {"type": "route", "settings": {"outbound": "direct"}}}
			],
			"default_action": {"type": "block"}
		}
	}`, freeLoopbackPort(t), echoPort)
	port, registry := startCountedSOCKS5(t, t.TempDir(), config)

	if !echoed(t, socks5Connect(t, port, "127.0.0.1", echoPort), "decided by the second rule") {
		t.Fatal("the connection was not relayed")
	}

	want := map[string]uint64{"routing.rules[0]": 0, "routing.rules[1]": 1, "default": 0}
	if got := registry.RuleHits(); !maps.Equal(got, want) {
		t.Errorf("rule hits = %v, want %v", got, want)
	}
}

func TestLiveConversationsAreListedOnlyWhenTheConfigSwitchesThemOn(t *testing.T) {
	echoPort := startTCPEchoServer(t)

	t.Run("on", func(t *testing.T) {
		port, registry := startCountedSOCKS5(t, t.TempDir(),
			statsConfig(freeLoopbackPort(t), startProbeTarget(t), `, "stats": {"tracking": {"enabled": true}}`))

		connection := socks5Connect(t, port, "127.0.0.1", echoPort)
		if !echoed(t, connection, "listed while open") {
			t.Fatal("the connection was not relayed")
		}
		if !registry.IsLiveTrackingEnabled() {
			t.Fatal("tracking is enabled, yet the registry does not track them")
		}
		var live []stats.ConversationSnapshot
		eventuallySnapshot(t, registry, "the connection listed", func(trafficView) bool {
			live = registry.LiveConversations()
			return len(live) == 1
		})
		if live[0].Info.InboundTag != "socks-in" || live[0].Info.Source.Addr().String() != "127.0.0.1" ||
			len(live[0].Info.OutboundTags) != 2 || live[0].Info.OutboundTags[0] != "auto" || live[0].Info.OutboundTags[1] != "direct" {
			t.Errorf("info = %+v, want socks-in from 127.0.0.1 through auto then direct", live[0].Info)
		}

		connection.Close()
		eventuallySnapshot(t, registry, "the connection gone after the client left",
			func(trafficView) bool { return len(registry.LiveConversations()) == 0 })
	})

	t.Run("off by default", func(t *testing.T) {
		port, registry := startCountedSOCKS5(t, t.TempDir(), statsConfig(freeLoopbackPort(t), startProbeTarget(t), ""))

		connection := socks5Connect(t, port, "127.0.0.1", echoPort)
		defer connection.Close()
		if !echoed(t, connection, "not listed") {
			t.Fatal("the connection was not relayed")
		}
		if registry.IsLiveTrackingEnabled() || len(registry.LiveConversations()) != 0 {
			t.Error("connections were listed without tracking")
		}
	})
}

func TestDialHealthTimesDialsOnlyWhenTheConfigSwitchesItOn(t *testing.T) {
	echoPort := startTCPEchoServer(t)

	t.Run("on", func(t *testing.T) {
		port, registry := startCountedSOCKS5(t, t.TempDir(),
			statsConfig(freeLoopbackPort(t), startProbeTarget(t), `, "stats": {"dial_health": true}`))
		connection := socks5Connect(t, port, "127.0.0.1", echoPort)
		defer connection.Close()
		if !echoed(t, connection, "timed") {
			t.Fatal("the connection was not relayed")
		}
		if got := registry.OutboundTraffic()["auto"]; got.TimedDials != 1 || got.TotalDialTime <= 0 {
			t.Errorf("auto = %+v, want one timed dial", got)
		}
	})

	t.Run("off by default", func(t *testing.T) {
		port, registry := startCountedSOCKS5(t, t.TempDir(), statsConfig(freeLoopbackPort(t), startProbeTarget(t), ""))
		connection := socks5Connect(t, port, "127.0.0.1", echoPort)
		defer connection.Close()
		if !echoed(t, connection, "not timed") {
			t.Fatal("the connection was not relayed")
		}
		if got := registry.OutboundTraffic()["auto"]; got.TimedDials != 0 {
			t.Errorf("auto = %+v, want no timed dials", got)
		}
	})
}

func TestTheResolverCountsIntoTheRegistryTheBuilderMade(t *testing.T) {
	configText := statsConfig(freeLoopbackPort(t), startProbeTarget(t), "")
	configPath := filepath.Join(t.TempDir(), "config.json")
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
	defer built.Close()
	counted := built.BuildStats(coreBlueprint)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	built.Resolver().ResolveDomain(ctx, "no-such-name.invalid")

	got := counted.DNS()
	if got.Lookups != 1 || got.Servers["system"].Queries == 0 {
		t.Errorf("dns = %+v, want one lookup and a query counted on the system server", got)
	}
}

func TestCountingCanBeSwitchedOffInTheConfig(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	port, registry := startCountedSOCKS5(t, t.TempDir(), statsConfig(freeLoopbackPort(t), startProbeTarget(t), `, "stats": {"enabled": false}`))

	if !echoed(t, socks5Connect(t, port, "127.0.0.1", echoPort), "not counted") {
		t.Fatal("the connection was not relayed with counting off")
	}
	if registry != nil {
		t.Errorf("registry = %+v, want nil with counting off", viewOf(registry))
	}
}

func TestStatsListsNarrowWhatIsCounted(t *testing.T) {
	echoPort := startTCPEchoServer(t)
	port, registry := startCountedSOCKS5(t, t.TempDir(),
		statsConfig(freeLoopbackPort(t), startProbeTarget(t), `, "stats": {"outbounds": ["direct"]}`))

	if !echoed(t, socks5Connect(t, port, "127.0.0.1", echoPort), "narrowed") {
		t.Fatal("the connection was not relayed")
	}

	snapshot := eventuallySnapshot(t, registry, "bytes counted under the member",
		func(snapshot trafficView) bool { return snapshot.Outbounds["direct"].DownlinkBytes > 0 })
	if _, counted := snapshot.Outbounds["auto"]; counted {
		t.Error("the group was counted though only direct is listed")
	}
	if snapshot.Inbounds["socks-in"].DownlinkBytes == 0 {
		t.Error("the inbound was not counted: only the outbound list narrows")
	}
}

func TestAConnectionThatCannotBeDialedIsCountedAsADialFailure(t *testing.T) {
	deadPort := freeLoopbackPort(t)
	port, registry := startCountedSOCKS5(t, t.TempDir(), statsConfig(freeLoopbackPort(t), startProbeTarget(t), ""))

	connection, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	request := []byte{0x05, 0x01, 0x00, 0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, byte(deadPort >> 8), byte(deadPort)}
	connection.Write(request)

	eventuallySnapshot(t, registry, "a dial failure counted on the group",
		func(snapshot trafficView) bool { return snapshot.Outbounds["auto"].DialFailures == 1 })
}

type trafficView struct {
	Inbounds  map[string]stats.InboundTraffic
	Outbounds map[string]stats.OutboundTraffic
	Users     map[string]stats.Traffic
}

func viewOf(registry *stats.Registry) trafficView {
	return trafficView{
		Inbounds:  registry.InboundTraffic(),
		Outbounds: registry.OutboundTraffic(),
		Users:     registry.UserTraffic(),
	}
}
