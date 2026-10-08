package apicoreprovider_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/apicoreprovider"
	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/logging"
	"github.com/The-Apranik/Apranik/core/logging/sink"
	"github.com/The-Apranik/Apranik/core/outbound/group"
	"github.com/The-Apranik/Apranik/core/stats"
	"github.com/The-Apranik/Apranik/spec"
)

// An outbound that reaches one address whatever it is asked for.
type fixedAddressOutbound struct {
	tag      string
	address  string
	networks []spec.Network
}

func (o fixedAddressOutbound) Tag() string { return o.tag }
func (o fixedAddressOutbound) Networks() []spec.Network {
	if o.networks != nil {
		return o.networks
	}
	return []spec.Network{spec.NetworkTCP}
}
func (o fixedAddressOutbound) Close() error { return nil }
func (o fixedAddressOutbound) CreateDatagram(context.Context, spec.Metadata) (spec.Datagram, error) {
	return nil, spec.ErrUnsupportedNetwork
}
func (o fixedAddressOutbound) DialStream(ctx context.Context, _ spec.Metadata) (spec.Stream, error) {
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", o.address)
	if err != nil {
		return nil, err
	}
	return connection.(*net.TCPConn), nil
}

type fakeProxyModule struct{ spec.StreamProxyModule }

func (fakeProxyModule) Info() spec.ModuleInfo { return spec.ModuleInfo{Name: "trojan"} }

// The configuration and the built outbounds of a small setup: a proxy, freedom
// and a selector over both.
type setup struct {
	provider      *apicoreprovider.Provider
	registry      *stats.Registry
	selector      component.Outbound
	proxy         fixedAddressOutbound
	freedom       fixedAddressOutbound
	loggerManager *logging.LoggerManager
}

func newSetup(t *testing.T, trackConnections bool, countStatistics bool, logOutputs ...logging.Output) *setup {
	t.Helper()
	proxy := fixedAddressOutbound{tag: "proxy", networks: []spec.Network{spec.NetworkTCP}}
	freedom := fixedAddressOutbound{tag: "direct", networks: []spec.Network{spec.NetworkTCP, spec.NetworkUDP}}
	selector, err := group.NewSelector(group.SelectorConfig{Tag: "choose", Members: []component.Outbound{proxy, freedom}})
	if err != nil {
		t.Fatal(err)
	}
	auto, err := group.NewFallback(group.FallbackConfig{Tag: "auto", Members: []component.Outbound{freedom}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { auto.Close() })

	system, err := logging.New(logging.Config{Level: logging.LevelInfo, Outputs: logOutputs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { system.Close() })

	described := &blueprint.Blueprint{
		Log: logging.Config{Level: logging.LevelNotice, Outputs: logOutputs},
		Outbounds: []blueprint.Outbound{
			{Tag: "proxy", Proxy: fakeProxyModule{}},
			{Tag: "direct", Freedom: &blueprint.Freedom{}},
			{Tag: "choose", Group: &blueprint.Group{Type: blueprint.GroupTypeSelector, Members: []string{"proxy", "direct"}}},
			{Tag: "auto", Group: &blueprint.Group{Type: blueprint.GroupTypeFallback, Members: []string{"direct"}}},
		},
		Routing: &blueprint.Routing{DefaultAction: blueprint.Action{Route: &blueprint.RouteAction{Outbound: "choose"}}},
	}
	var registry *stats.Registry
	if countStatistics {
		registry = stats.NewRegistry(stats.Config{
			InboundTags: []string{"in"}, TrackLiveConversations: trackConnections,
			Outbounds: []stats.OutboundDescription{{Tag: "proxy"}, {Tag: "direct"}, {Tag: "choose", IsGroup: true}, {Tag: "auto", IsGroup: true}},
		})
	}
	provider, err := apicoreprovider.New(apicoreprovider.Parts{
		Version: "9.9.9", Stats: registry, Logs: system.SubscribeToLog, Blueprint: described,
		Outbounds: []component.Outbound{proxy, freedom, selector, auto},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &setup{provider: provider, registry: registry, selector: selector, proxy: proxy, freedom: freedom, loggerManager: system}
}

func TestTheProviderNeedsABlueprintAndOneOutboundPerDescription(t *testing.T) {
	if _, err := apicoreprovider.New(apicoreprovider.Parts{}); err == nil {
		t.Error("a provider was built with no blueprint")
	}
	if _, err := apicoreprovider.New(apicoreprovider.Parts{Blueprint: &blueprint.Blueprint{Outbounds: []blueprint.Outbound{{Tag: "a"}}}}); err == nil {
		t.Error("a provider was built with fewer outbounds than the blueprint describes")
	}
}

func TestVersionAndConfiguredLogLevel(t *testing.T) {
	s := newSetup(t, true, true)
	if s.provider.ApranikVersion() != "9.9.9" {
		t.Errorf("version = %q", s.provider.ApranikVersion())
	}
	if s.provider.ConfiguredLogLevel() != logging.LevelNotice {
		t.Errorf("log level = %v", s.provider.ConfiguredLogLevel())
	}
}

func TestTotalsAreTheCountedBytesAndNothingWithoutStatistics(t *testing.T) {
	s := newSetup(t, false, true)
	totals, err := s.provider.TotalTraffic()
	if err != nil || totals != (spec.APITrafficTotal{}) {
		t.Errorf("TotalTraffic = %+v, %v; want zero bytes and no error", totals, err)
	}
	if _, err := s.provider.TotalTCPTraffic(); err != nil {
		t.Error(err)
	}
	if _, err := s.provider.TotalUDPTraffic(); err != nil {
		t.Error(err)
	}

	disabled := newSetup(t, false, false)
	for name, read := range map[string]func() (spec.APITrafficTotal, error){
		"all": disabled.provider.TotalTraffic, "tcp": disabled.provider.TotalTCPTraffic, "udp": disabled.provider.TotalUDPTraffic,
	} {
		if _, err := read(); !errors.Is(err, spec.ErrAPIStatisticsDisabled) {
			t.Errorf("%s with statistics off = %v, want ErrAPIStatisticsDisabled", name, err)
		}
	}
}

func TestLiveConversationsAreConvertedFieldForField(t *testing.T) {
	s := newSetup(t, true, true)
	destination, _ := spec.NewDestinationFromDomain("example.com", 443)
	connection := s.registry.TrackConversation(stats.ConversationInfo{
		Network: spec.NetworkTCP, InboundTag: "in", InboundProtocol: "socks5", User: "alice", Destination: destination,
		SniffedDomain: "sniffed.example", OutboundTags: []string{"choose", "proxy"}, Rule: "routing.rules[3]",
	}, nil)
	connection.Bytes().AddUplinkBytes(7)
	connection.Bytes().AddDownlinkBytes(9)

	live, err := s.provider.LiveConversations()
	if err != nil || len(live) != 1 {
		t.Fatalf("LiveConnections = %v, %v", live, err)
	}
	got := live[0]
	if got.ID == "" || got.Network != spec.NetworkTCP || got.InboundTag != "in" || got.InboundProtocol != "socks5" || got.SniffedDomain != "sniffed.example" || got.User != "alice" || got.Destination != destination ||
		got.RuleLocation != "routing.rules[3]" || got.UplinkBytes != 7 || got.DownlinkBytes != 9 || len(got.OutboundTags) != 2 || got.StartedAt.IsZero() {
		t.Errorf("connection = %+v", got)
	}
}

func TestLiveConversationsNameWhyTheyAreNotListed(t *testing.T) {
	if _, err := newSetup(t, true, false).provider.LiveConversations(); !errors.Is(err, spec.ErrAPIStatisticsDisabled) {
		t.Errorf("statistics off: %v", err)
	}
	if _, err := newSetup(t, false, true).provider.LiveConversations(); !errors.Is(err, spec.ErrAPILiveConversationsDisabled) {
		t.Errorf("live conversations off: %v", err)
	}
}

func TestClosingConversationsAsksThemToEndAndNamesWhyItCould(t *testing.T) {
	s := newSetup(t, true, true)
	var closed atomic.Int32
	connection := s.registry.TrackConversation(stats.ConversationInfo{}, func() { closed.Add(1) })
	live, _ := s.provider.LiveConversations()

	if err := s.provider.CloseLiveConversation(live[0].ID); err != nil || closed.Load() != 1 {
		t.Errorf("CloseLiveConversation = %v, closer calls %d; want nil and 1", err, closed.Load())
	}
	if err := s.provider.CloseLiveConversation("999"); !errors.Is(err, spec.ErrAPINoSuchLiveConversation) {
		t.Errorf("an unknown id = %v, want ErrAPINoSuchLiveConversation", err)
	}
	if err := s.provider.CloseAllLiveConversations(); err != nil || closed.Load() != 2 {
		t.Errorf("CloseAllLiveConversations = %v, closer calls %d; want nil and 2", err, closed.Load())
	}
	connection.End()

	off := newSetup(t, false, true).provider
	if err := off.CloseLiveConversation("1"); !errors.Is(err, spec.ErrAPILiveConversationsDisabled) {
		t.Errorf("live conversations off, CloseLiveConversation = %v", err)
	}
	if err := off.CloseAllLiveConversations(); !errors.Is(err, spec.ErrAPILiveConversationsDisabled) {
		t.Errorf("live conversations off, CloseAllLiveConversations = %v", err)
	}
	none := newSetup(t, true, false).provider
	if err := none.CloseLiveConversation("1"); !errors.Is(err, spec.ErrAPIStatisticsDisabled) {
		t.Errorf("statistics off, CloseLiveConversation = %v", err)
	}
}

func TestLogsReachASubscriberAtItsLevel(t *testing.T) {
	s := newSetup(t, true, true)
	subscription, err := s.provider.SubscribeToLog(slog.LevelDebug, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()
	s.loggerManager.Logger().Debug("hello")

	select {
	case record := <-subscription.Records():
		if record.Message != "hello" {
			t.Errorf("record = %q", record.Message)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no record")
	}
	if _, err := s.provider.SubscribeToLog(slog.LevelDebug, 0); err == nil {
		t.Error("a subscription with no buffer was accepted")
	}
}

func TestTheLogFilePathIsTheNthFileOutputMadeAbsolute(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	first, second := filepath.Join(directory, "a.log"), "relative.log"
	s := newSetup(t, true, true, sink.FileOutput{Path: first}, sink.StderrOutput{}, sink.FileOutput{Path: second})
	t.Cleanup(func() { os.Remove(first) })

	if path, err := s.provider.LogFilePath(0); err != nil || path != first {
		t.Errorf("LogFilePath(0) = %q, %v; want %q", path, err, first)
	}
	want, _ := filepath.Abs(second)
	if path, err := s.provider.LogFilePath(1); err != nil || path != want {
		t.Errorf("LogFilePath(1) = %q, %v; want %q", path, err, want)
	}
	for _, index := range []int{2, -1} {
		if _, err := s.provider.LogFilePath(index); !errors.Is(err, spec.ErrAPINoLogFileOutput) {
			t.Errorf("LogFilePath(%d) = %v, want ErrAPINoLogFileOutput", index, err)
		}
	}
}

func TestOutboundsAreListedInConfigurationOrderWithTheirState(t *testing.T) {
	s := newSetup(t, true, true)
	tags := s.provider.OutboundTags()
	if len(tags) != 4 || tags[0] != "proxy" || tags[3] != "auto" {
		t.Errorf("OutboundTags = %v", tags)
	}

	for _, tc := range []struct {
		tag           string
		kind          spec.APIOutboundKind
		udp           bool
		groupOutbound bool
		current       string
	}{
		{"proxy", spec.APIOutboundKindProxy, false, false, ""},
		{"direct", spec.APIOutboundKindFreedom, true, false, ""},
		{"choose", spec.APIOutboundKindSelector, false, true, "proxy"},
		{"auto", spec.APIOutboundKindFallback, true, true, "direct"},
	} {
		state, err := s.provider.OutboundState(tc.tag)
		if err != nil {
			t.Fatal(err)
		}
		if state.Tag != tc.tag || state.Kind != tc.kind || state.CarriesUDP != tc.udp || (state.Group != nil) != tc.groupOutbound {
			t.Errorf("%s = %+v", tc.tag, state)
		}
		if tc.groupOutbound && state.Group.CurrentMember != tc.current {
			t.Errorf("%s current member = %q, want %q", tc.tag, state.Group.CurrentMember, tc.current)
		}
	}
	if state, _ := s.provider.OutboundState("proxy"); state.ProtocolName != "trojan" {
		t.Errorf("protocol name = %q, want trojan", state.ProtocolName)
	}
	if state, _ := s.provider.OutboundState("choose"); len(state.Group.MemberTags) != 2 {
		t.Errorf("selector members = %v", state.Group.MemberTags)
	}
	if _, err := s.provider.OutboundState("nowhere"); !errors.Is(err, spec.ErrAPINoSuchOutbound) {
		t.Errorf("an unknown outbound = %v", err)
	}
}

func TestTheDefaultOutboundIsTheDefaultActionsOrTheFirst(t *testing.T) {
	s := newSetup(t, true, true)
	if got := s.provider.DefaultOutboundTag(); got != "choose" {
		t.Errorf("default = %q, want choose", got)
	}
}

func answering(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(status) }))
}

func TestMeasureDelayTimesOneRequestThroughTheOutbound(t *testing.T) {
	server := answering(http.StatusNoContent)
	t.Cleanup(server.Close)
	s := newSetup(t, true, true)
	s.proxy.address = server.Listener.Addr().String()
	provider := rebuildWith(t, s, s.proxy)

	delay, err := provider.MeasureDelay(context.Background(), "proxy", server.URL+"/", 5*time.Second, "200-299")
	if err != nil || delay <= 0 {
		t.Errorf("MeasureDelay = %v, %v; want a positive delay", delay, err)
	}
	if _, err := provider.MeasureDelay(context.Background(), "proxy", server.URL+"/", 5*time.Second, "200"); err == nil {
		t.Error("a 204 passed a test that expects 200")
	}
	if _, err := provider.MeasureDelay(context.Background(), "proxy", server.URL+"/", 5*time.Second, "100-599"); err != nil {
		t.Errorf("any status should pass 100-599: %v", err)
	}
}

// The outbounds of a setup with a new proxy, which knows where to dial.
func rebuildWith(t *testing.T, s *setup, proxy fixedAddressOutbound) *apicoreprovider.Provider {
	t.Helper()
	described := &blueprint.Blueprint{Outbounds: []blueprint.Outbound{{Tag: "proxy", Proxy: fakeProxyModule{}}}}
	provider, err := apicoreprovider.New(apicoreprovider.Parts{Blueprint: described, Outbounds: []component.Outbound{proxy}})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestMeasureDelayRefusesARequestItCannotMakeAndKnowsNoDefaults(t *testing.T) {
	s := newSetup(t, true, true)
	for name, tc := range map[string]struct {
		url      string
		timeout  time.Duration
		expected string
	}{
		"no url":            {"", time.Second, "200-299"},
		"ftp":               {"ftp://example.com/", time.Second, "200-299"},
		"no timeout":        {"http://127.0.0.1:1/", 0, "200-299"},
		"negative timeout":  {"http://127.0.0.1:1/", -time.Second, "200-299"},
		"no statuses":       {"http://127.0.0.1:1/", time.Second, ""},
		"a bad status":      {"http://127.0.0.1:1/", time.Second, "700"},
		"a backwards range": {"http://127.0.0.1:1/", time.Second, "300-200"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.provider.MeasureDelay(context.Background(), "proxy", tc.url, tc.timeout, tc.expected)
			if !errors.Is(err, spec.ErrAPIInvalidDelayTest) {
				t.Errorf("err = %v, want ErrAPIInvalidDelayTest", err)
			}
		})
	}
	if _, err := s.provider.MeasureDelay(context.Background(), "nowhere", "http://127.0.0.1:1/", time.Second, "200"); !errors.Is(err, spec.ErrAPINoSuchOutbound) {
		t.Errorf("an unknown outbound = %v", err)
	}
}

func TestMeasureDelayReportsATimeoutAsADeadline(t *testing.T) {
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { silent.Close() })
	go func() {
		for {
			connection, err := silent.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { connection.Close() })
		}
	}()
	s := newSetup(t, true, true)
	provider := rebuildWith(t, s, fixedAddressOutbound{tag: "proxy", address: silent.Addr().String()})

	_, err = provider.MeasureDelay(context.Background(), "proxy", "http://127.0.0.1:1/", 100*time.Millisecond, "200-299")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a silent target = %v, want an error wrapping context.DeadlineExceeded", err)
	}
}

func TestSelectingAMemberMovesTheSelectorAndNamesWhyItCouldNot(t *testing.T) {
	s := newSetup(t, false, false)
	if err := s.provider.SelectMember("choose", "direct"); err != nil {
		t.Fatal(err)
	}
	if state, _ := s.provider.OutboundState("choose"); state.Group.CurrentMember != "direct" {
		t.Errorf("current member = %q, want direct", state.Group.CurrentMember)
	}

	for name, tc := range map[string]struct {
		selector, member string
		want             error
	}{
		"an unknown outbound": {"nowhere", "direct", spec.ErrAPINoSuchOutbound},
		"a fallback group":    {"auto", "direct", spec.ErrAPIOutboundNotSelectable},
		"a freedom outbound":  {"direct", "proxy", spec.ErrAPIOutboundNotSelectable},
		"an unknown member":   {"choose", "nowhere", spec.ErrAPINoSuchMember},
	} {
		if err := s.provider.SelectMember(tc.selector, tc.member); !errors.Is(err, tc.want) {
			t.Errorf("%s: SelectMember = %v, want %v", name, err, tc.want)
		}
	}
	if state, _ := s.provider.OutboundState("choose"); state.Group.CurrentMember != "direct" {
		t.Error("a refused switch moved the selector")
	}
}

func TestASwitchEndsExactlyTheConnectionsThatWentThroughThePreviousMember(t *testing.T) {
	s := newSetup(t, true, true)
	closers := map[string]*atomic.Int32{}
	track := func(name string, tags ...string) {
		var closed atomic.Int32
		closers[name] = &closed
		s.registry.TrackConversation(stats.ConversationInfo{OutboundTags: tags}, func() { closed.Add(1) })
	}
	track("through the old member", "choose", "proxy")
	track("through the old member, itself a group", "choose", "proxy", "deeper")
	track("through another member of the selector", "choose", "inner", "proxy")
	track("through the new member", "choose", "direct")
	track("to the old member by another route", "proxy")
	track("through another selector", "other", "proxy")
	track("through the selector and a lookalike", "choose", "proxy2")

	if err := s.provider.SelectMember("choose", "direct"); err != nil {
		t.Fatal(err)
	}

	for name, closed := range closers {
		want := name == "through the old member" || name == "through the old member, itself a group"
		if closed.Load() != 0 && !want {
			t.Errorf("%q was closed by the switch", name)
		}
		if closed.Load() == 0 && want {
			t.Errorf("%q was not closed by the switch", name)
		}
	}
}

func TestASwitchWithoutLiveConversationsStillSwitches(t *testing.T) {
	s := newSetup(t, false, true)
	if err := s.provider.SelectMember("choose", "direct"); err != nil {
		t.Fatalf("a switch with live conversations off failed: %v", err)
	}
}
