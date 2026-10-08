package connections_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/connections"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/stream"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/testkit"
	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func mount(t *testing.T, provider *testkit.Provider) *testkit.Env {
	return testkit.Mount(t, func(router *web.Router, hub *stream.Hub) { connections.Register(router, provider, hub) })
}

type connectionsReply struct {
	DownloadTotal uint64 `json:"downloadTotal"`
	UploadTotal   uint64 `json:"uploadTotal"`
	Memory        uint64 `json:"memory"`
	Connections   []struct {
		ID       string `json:"id"`
		Metadata struct {
			Network         string `json:"network"`
			Type            string `json:"type"`
			SourceIP        string `json:"sourceIP"`
			SourcePort      string `json:"sourcePort"`
			DestinationIP   string `json:"destinationIP"`
			DestinationPort string `json:"destinationPort"`
			InboundName     string `json:"inboundName"`
			InboundUser     string `json:"inboundUser"`
			Host            string `json:"host"`
			SniffHost       string `json:"sniffHost"`
			DNSMode         string `json:"dnsMode"`
			ProcessPath     string `json:"processPath"`
		} `json:"metadata"`
		Upload      uint64    `json:"upload"`
		Download    uint64    `json:"download"`
		Start       time.Time `json:"start"`
		Chains      []string  `json:"chains"`
		Rule        string    `json:"rule"`
		RulePayload string    `json:"rulePayload"`
	} `json:"connections"`
}

func endpointOf(t *testing.T, address string) spec.Endpoint {
	t.Helper()
	endpoint, err := spec.NewEndpoint(netip.MustParseAddrPort(address).Addr(), netip.MustParseAddrPort(address).Port())
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func conversationFixtures(t *testing.T) []spec.APILiveConversation {
	domainDestination, _ := spec.NewDestinationFromDomain("example.com", 443)
	ipDestination, _ := spec.NewDestinationFromIP(netip.MustParseAddr("203.0.113.9"), 8080)
	return []spec.APILiveConversation{
		{
			ID: "1", Network: spec.NetworkTCP, InboundTag: "in", InboundProtocol: "socks5", User: "alice",
			Source: endpointOf(t, "192.0.2.7:51000"), Destination: domainDestination,
			StartedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), OutboundTags: []string{"auto", "server-a"},
			RuleLocation: "routing.rules[3]", UplinkBytes: 11, DownlinkBytes: 22,
		},
		{
			ID: "2", Network: spec.NetworkUDP, InboundTag: "in2", InboundProtocol: "mixed",
			Source: endpointOf(t, "[2001:db8::7]:5353"), Destination: ipDestination, SniffedDomain: "sniffed.example",
			OutboundTags: []string{"direct"}, RuleLocation: "default",
		},
	}
}

func TestConnectionsListsEveryLiveConversationInClashShape(t *testing.T) {
	provider := &testkit.Provider{
		TrafficFunc: func() (spec.APITrafficTotal, error) {
			return spec.APITrafficTotal{UplinkBytes: 100, DownlinkBytes: 200}, nil
		},
		ConversationsFunc: func() ([]spec.APILiveConversation, error) { return conversationFixtures(t), nil },
	}
	server := mount(t, provider)

	var reply connectionsReply
	if status := server.GetJSON("/connections", &reply); status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if reply.UploadTotal != 100 || reply.DownloadTotal != 200 || reply.Memory == 0 || len(reply.Connections) != 2 {
		t.Fatalf("reply = %+v", reply)
	}

	first := reply.Connections[0]
	if first.ID != "1" || first.Metadata.Network != "tcp" || first.Metadata.Type != "Socks5" ||
		first.Metadata.SourceIP != "192.0.2.7" || first.Metadata.SourcePort != "51000" ||
		first.Metadata.DestinationIP != "" || first.Metadata.DestinationPort != "443" || first.Metadata.Host != "example.com" ||
		first.Metadata.InboundName != "in" || first.Metadata.InboundUser != "alice" || first.Metadata.DNSMode != "normal" ||
		first.Metadata.ProcessPath != "" || first.Upload != 11 || first.Download != 22 ||
		first.Rule != "routing.rules[3]" || first.RulePayload != "" || !first.Start.Equal(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("first = %+v", first)
	}
	if len(first.Chains) != 2 || first.Chains[0] != "server-a" || first.Chains[1] != "auto" {
		t.Errorf("chains = %v, want the carrying proxy first and the group last", first.Chains)
	}

	second := reply.Connections[1]
	if second.Metadata.Network != "udp" || second.Metadata.Type != "Mixed" || second.Metadata.SourceIP != "2001:db8::7" ||
		second.Metadata.DestinationIP != "203.0.113.9" || second.Metadata.DestinationPort != "8080" ||
		second.Metadata.Host != "sniffed.example" || second.Metadata.SniffHost != "sniffed.example" {
		t.Errorf("second = %+v (the sniffed name is the host when the destination is an IP)", second.Metadata)
	}
}

func TestConnectionsHostPrefersTheSniffedNameOverTheDomainTheClientSent(t *testing.T) {
	destination, _ := spec.NewDestinationFromDomain("front.example", 443)
	provider := &testkit.Provider{ConversationsFunc: func() ([]spec.APILiveConversation, error) {
		return []spec.APILiveConversation{{ID: "1", Destination: destination, SniffedDomain: "real.example"}}, nil
	}}
	var reply connectionsReply
	mount(t, provider).GetJSON("/connections", &reply)
	if reply.Connections[0].Metadata.Host != "real.example" {
		t.Errorf("host = %q", reply.Connections[0].Metadata.Host)
	}
}

func TestConnectionsWithNothingLiveIsAnEmptyListNotNull(t *testing.T) {
	server := mount(t, &testkit.Provider{})
	_, body, _ := server.Do("GET", "/connections", "")
	if !strings.Contains(body, `"connections":[]`) {
		t.Errorf("body = %q, want an empty list", body)
	}
}

func TestConnectionsExplainsWhyItCannotListThem(t *testing.T) {
	for name, test := range map[string]struct {
		err  error
		want string
	}{
		"statistics off": {spec.ErrAPIStatisticsDisabled, "statistics are disabled"},
		"tracking off":   {spec.ErrAPILiveConversationsDisabled, "stats.tracking.enabled"},
	} {
		server := mount(t, &testkit.Provider{ConversationsFunc: func() ([]spec.APILiveConversation, error) { return nil, test.err }})
		status, body, _ := server.Do("GET", "/connections", "")
		if status != http.StatusServiceUnavailable || !strings.Contains(testkit.MessageOf(t, body), test.want) {
			t.Errorf("%s: %d %q", name, status, body)
		}
	}
}

func TestConnectionsOverAWebSocketPushesASnapshotEveryInterval(t *testing.T) {
	provider := &testkit.Provider{ConversationsFunc: func() ([]spec.APILiveConversation, error) { return conversationFixtures(t), nil }}
	server := mount(t, provider)

	socket, err := server.DialWebSocket("/connections?interval=100", "")
	if err != nil {
		t.Fatalf("the upgrade was refused: %v", err)
	}
	for range 3 {
		var reply connectionsReply
		if err := json.Unmarshal([]byte(testkit.ReadText(t, socket)), &reply); err != nil || len(reply.Connections) != 2 {
			t.Fatalf("snapshot %+v, %v", reply, err)
		}
	}
}

func TestConnectionsRefusesAnIntervalThatIsNotAWholeNumberOfMilliseconds(t *testing.T) {
	server := mount(t, &testkit.Provider{})
	for _, interval := range []string{"fast", "0", "-5", "99", "1.5"} {
		if status := server.UpgradeStatus("/connections?interval=" + interval); status != http.StatusBadRequest {
			t.Errorf("interval %q: the upgrade got %d, want 400", interval, status)
		}
	}
}

func TestClosingConnections(t *testing.T) {
	var closedOne string
	closedAll := 0
	provider := &testkit.Provider{
		CloseOneFunc: func(id string) error {
			if id == "404" {
				return spec.ErrAPINoSuchLiveConversation
			}
			if id == "off" {
				return spec.ErrAPILiveConversationsDisabled
			}
			closedOne = id
			return nil
		},
		CloseAllFunc: func() error { closedAll++; return nil },
	}
	server := mount(t, provider)

	if status, body, _ := server.Do("DELETE", "/connections/17", ""); status != http.StatusNoContent || body != "" || closedOne != "17" {
		t.Errorf("DELETE one = %d %q (closed %q)", status, body, closedOne)
	}
	if status, _, _ := server.Do("DELETE", "/connections/404", ""); status != http.StatusNotFound {
		t.Errorf("an unknown id: %d, want 404", status)
	}
	if status, body, _ := server.Do("DELETE", "/connections/off", ""); status != http.StatusServiceUnavailable || !strings.Contains(testkit.MessageOf(t, body), "stats.tracking.enabled") {
		t.Errorf("tracking off: %d %q", status, body)
	}
	if status, _, _ := server.Do("DELETE", "/connections", ""); status != http.StatusNoContent || closedAll != 1 {
		t.Errorf("DELETE all = %d, closed all %d times", status, closedAll)
	}

	failing := mount(t, &testkit.Provider{CloseAllFunc: func() error { return spec.ErrAPILiveConversationsDisabled }})
	if status, _, _ := failing.Do("DELETE", "/connections", ""); status != http.StatusServiceUnavailable {
		t.Errorf("DELETE all with tracking off = %d, want 503 and never a silent success", status)
	}
	other := mount(t, &testkit.Provider{CloseOneFunc: func(string) error { return errors.New("boom") }})
	if status, _, _ := other.Do("DELETE", "/connections/1", ""); status != http.StatusInternalServerError {
		t.Errorf("an unexpected error = %d, want 500", status)
	}
}
