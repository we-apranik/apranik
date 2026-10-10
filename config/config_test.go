package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/config"
	"github.com/The-Apranik/Apranik/core/net/udp/nat"
	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/module/proxy/socks5"
	"github.com/The-Apranik/Apranik/module/proxy/trojan"
	"github.com/The-Apranik/Apranik/module/transport/base/tcp"
	"github.com/The-Apranik/Apranik/spec"
)

func modules(t *testing.T) *registry.Registry {
	t.Helper()

	registered, err := registry.Default()
	if err != nil {
		t.Fatalf("registry.Default: %v", err)
	}
	return registered
}

// A config file that should load and check cleanly, used as the base for the
// failure cases below.
const goodDocument = `{
  "version": 1,
  "log": {"level": "info", "outputs": [{"type": "stderr"}]},
  "inbounds": [{
    "tag": "socks-in",
    "listen": "127.0.0.1",
    "port": 1080,
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}}
  }],
  "outbounds": [{
    "tag": "freedom",
    "protocol": {"type": "freedom"}
  }]
}`

func loadAndCheck(t *testing.T, document string) (config.Diagnostics, bool) {
	t.Helper()

	registered := modules(t)
	configuration, diagnostics := config.Parse([]byte(document), registered)
	if configuration != nil {
		diagnostics = append(diagnostics, config.Check(configuration)...)
	}
	return diagnostics, diagnostics.HasErrors()
}

func requireNoErrors(t *testing.T, diagnostics config.Diagnostics) {
	t.Helper()

	if diagnostics.HasErrors() {
		t.Fatalf("unexpected errors:\n%v", diagnostics.Err())
	}
}

// Finds a diagnostic whose path and message both match, so a test cannot pass
// on an unrelated failure that happens to be present.
func find(diagnostics config.Diagnostics, path, substring string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Location == path && strings.Contains(diagnostic.Message, substring) {
			return true
		}
	}
	return false
}

func TestGoodDocumentLoads(t *testing.T) {
	registered := modules(t)

	configuration, diagnostics := config.Parse([]byte(goodDocument), registered)
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))

	if len(configuration.Inbounds) != 1 || len(configuration.Outbounds) != 1 {
		t.Fatalf("got %d inbounds and %d outbounds, want 1 each",
			len(configuration.Inbounds), len(configuration.Outbounds))
	}

	inbound := configuration.Inbounds[0]
	if inbound.Bind.Port() != 1080 || !inbound.Bind.Addr().IsLoopback() {
		t.Errorf("bind = %v, want 127.0.0.1:1080", inbound.Bind)
	}
	if inbound.Network != spec.NetworkTCP {
		t.Errorf("network = %q, want tcp by default", inbound.Network)
	}
	// The settings really reached the module, through the embedded struct.
	proxy, ok := inbound.StreamProxy.(*socks5.Proxy)
	if !ok {
		t.Fatalf("proxy is %T, want *socks5.Proxy", inbound.StreamProxy)
	}
	if len(proxy.Users) != 1 || proxy.Users[0].Name != "u" || proxy.Users[0].Password.Reveal() != "p" {
		t.Error("proxy settings did not decode into the promoted fields")
	}
	if !proxy.UDPRelay {
		t.Error("udp_relay lost its default; absent keys must not overwrite the constructor")
	}
}

// ---------- the envelope ----------

func TestVersionIsRequired(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, `{"inbounds": []}`)
	if !failed {
		t.Fatal("a config file with no version was accepted")
	}
	if !find(diagnostics, "version", "schema version") {
		t.Errorf("diagnostics do not explain the missing version: %v", diagnostics)
	}
}

func TestUnsupportedVersionIsNamed(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, `{"version": 2}`)
	if !failed {
		t.Fatal("an unsupported version was accepted")
	}
	if !find(diagnostics, "version", "not supported by this core") {
		t.Errorf("diagnostics do not name the unsupported version: %v", diagnostics)
	}
}

// An old core reading a new config file should say exactly that, rather than
// complaining about a field it has never heard of.
func TestMinCoreIsCheckedBeforeAnyField(t *testing.T) {
	document := `{"version": 1, "min_core": "99.0.0", "inbounds": [{"a_field_from_the_future": true}]}`

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a config file requiring a newer core was accepted")
	}
	if !find(diagnostics, "min_core", "requires Apranik") {
		t.Errorf("diagnostics do not explain the version requirement: %v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "a_field_from_the_future") {
			t.Error("the unknown field was reported, so min_core was not checked first")
		}
	}
}

func TestSatisfiedMinCorePasses(t *testing.T) {
	document := strings.Replace(goodDocument, `"version": 1,`, `"version": 1, "min_core": "0.0.1",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("a satisfiable min_core was refused: %v", diagnostics.Err())
	}
}

// ---------- strictness ----------

// A silently ignored field ships behaviour the user never asked for: someone
// who writes "fingerprnt" believes they configured a fingerprint.
func TestUnknownSettingIsRefusedByDefault(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"settings": {"users": [{"name": "u", "password": "p"}]}`,
		`"settings": {"usernam": "u"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a misspelled setting was accepted under the default strictness")
	}
	if !find(diagnostics, "inbounds[0].protocol.settings", "usernam") {
		t.Errorf("diagnostics do not name the offending field and path: %v", diagnostics)
	}
}

func TestStrictFalseAcceptsUnknownSettings(t *testing.T) {
	document := strings.Replace(goodDocument, `"version": 1,`, `"version": 1, "strict": false,`, 1)
	document = strings.Replace(document,
		`"settings": {"users": [{"name": "u", "password": "p"}]}`,
		`"settings": {"users": [{"name": "u", "password": "p"}], "usernam": "typo"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("strict:false still refused an unknown field: %v", diagnostics.Err())
	}
}

// ---------- kind comes from position ----------

func TestABaseInAnOverlaySlotSaysWhatItActuallyIs(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"transport": [{"type": "tcp"}],
    "protocol"`,
		`"transport": [{"type": "tcp"}, {"type": "tcp"}],
    "protocol"`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a base transport in an overlay slot was accepted")
	}
	if !find(diagnostics, "inbounds[0].transport[1]", "base stream transport") {
		t.Errorf("the error does not say what tcp actually is: %v", diagnostics)
	}
}

func TestUnknownTransportIsReportedWithItsPath(t *testing.T) {
	document := strings.Replace(goodDocument, `"transport": [{"type": "tcp"}],
    "protocol"`, `"transport": [{"type": "nonesuch"}],
    "protocol"`, 1)

	diagnostics, _ := loadAndCheck(t, document)
	if !find(diagnostics, "inbounds[0].transport[0]", "nonesuch") {
		t.Errorf("diagnostics do not point at the bad transport: %v", diagnostics)
	}
}

// One run should surface every problem, not the first.
func TestDecodingContinuesPastAFailure(t *testing.T) {
	document := `{
	  "version": 1,
	  "inbounds": [
	    {"tag": "a", "listen": "127.0.0.1", "port": 1080, "transport": [{"type": "nope"}], "protocol": {"type": "socks5"}},
	    {"tag": "b", "listen": "127.0.0.1", "port": 1081, "transport": [{"type": "tcp"}], "protocol": {"type": "alsonope"}}
	  ],
	  "outbounds": [{"tag": "freedom", "transport": [{"type": "tcp"}]}]
	}`

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("two broken inbounds were accepted")
	}
	if !find(diagnostics, "inbounds[0].transport[0]", "nope") {
		t.Errorf("the first failure was not reported: %v", diagnostics)
	}
	if !find(diagnostics, "inbounds[1].protocol", "alsonope") {
		t.Errorf("decoding stopped at the first failure: %v", diagnostics)
	}
}

// ---------- listen and server ----------

func TestListenIsRequiredWithNoDefault(t *testing.T) {
	document := strings.Replace(goodDocument, `"listen": "127.0.0.1",`, "", 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("an inbound with no listen address was accepted")
	}
	if !find(diagnostics, "inbounds[0].listen", "no default") {
		t.Errorf("diagnostics do not explain that listen is required: %v", diagnostics)
	}
}

func TestPortInsideListenIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument, `"listen": "127.0.0.1",`, `"listen": "127.0.0.1:1080",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a host:port listen was accepted")
	}
	if !find(diagnostics, "inbounds[0].listen", "own field") {
		t.Errorf("diagnostics do not explain the separate port field: %v", diagnostics)
	}
}

// Separate fields are most of why a bare IPv6 literal needs no brackets.
// Resolving is I/O, which this package does not do, and a domain that answers
// with several addresses leaves "which one did it bind?" unanswerable.
func TestADomainListenAddressIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument, `"listen": "127.0.0.1",`, `"listen": "localhost",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a domain listen address was accepted")
	}
	if !find(diagnostics, "inbounds[0].listen", "cannot be a domain") {
		t.Errorf("diagnostics do not explain that listen must be an IP: %v", diagnostics)
	}
}

// A proxy server, unlike a listen address, may be a domain: it is dialled, not
// bound, and the transport resolves it.
func TestADomainServerAddressIsAccepted(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"tag": "freedom",
    "protocol": {"type": "freedom"}`,
		`"tag": "proxied",
    "server": "proxy.example.com",
    "server_port": 443,
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("a domain server address was refused: %v", diagnostics.Err())
	}
}

func TestBareIPv6Listen(t *testing.T) {
	document := strings.Replace(goodDocument, `"listen": "127.0.0.1",`, `"listen": "::1",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("a bare IPv6 listen address was refused: %v", diagnostics.Err())
	}
}

func TestFreedomOutboundMayNotNameAServer(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"tag": "freedom",`,
		`"tag": "freedom", "server": "example.com", "server_port": 443,`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a freedom outbound with a server address was accepted")
	}
	if !find(diagnostics, "outbounds[0].server", "takes no server") {
		t.Errorf("diagnostics do not explain why: %v", diagnostics)
	}
}

func TestProxyOutboundNeedsAServer(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"tag": "freedom",
    "protocol": {"type": "freedom"}`,
		`"tag": "upstream",
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a proxy outbound with no server was accepted")
	}
	if !find(diagnostics, "outbounds[0].server", "no server address") {
		t.Errorf("diagnostics do not explain the missing server: %v", diagnostics)
	}
}

func freedomWithTransport(transport string) string {
	return strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"transport": `+transport+`, "protocol": {"type": "freedom"}`, 1)
}

func TestFreedomTakesTCPAndFragment(t *testing.T) {
	document := freedomWithTransport(`[{"type": "tcp", "settings": {"keep_alive": "15s"}}, {"type": "fragment", "settings": {"piece_sizes": ["1-3", "20"]}}]`)
	registered := modules(t)
	configuration, diagnostics := config.Parse([]byte(document), registered)
	diagnostics = append(diagnostics, config.Check(configuration)...)
	requireNoErrors(t, diagnostics)

	stack := configuration.Outbounds[0].Freedom.StreamTransport
	if stack.Base == nil || stack.Base.Info().Name != "tcp" || len(stack.Overlays) != 1 || stack.Overlays[0].Info().Name != "fragment" {
		t.Errorf("freedom's stack is not tcp, fragment: %+v", stack)
	}
}

func TestFreedomWithoutTransportIsPlainTCP(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(goodDocument), modules(t))
	requireNoErrors(t, diagnostics)
	stack := configuration.Outbounds[0].Freedom.StreamTransport
	if stack.Base == nil || stack.Base.Info().Name != "tcp" || len(stack.Overlays) != 0 {
		t.Errorf("freedom's default stack is not plain tcp: %+v", stack)
	}
}

func TestFreedomRefusesATransportThatNeedsAServerSide(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, freedomWithTransport(`[{"type": "tcp"}, {"type": "ws"}]`))
	if !failed || !find(diagnostics, "outbounds[0].transport[1]", `"ws" cannot be used with freedom`) ||
		!find(diagnostics, "outbounds[0].transport[1]", "Transports that work with freedom: tcp, fragment") {
		t.Errorf("ws under freedom was not refused clearly: %v", diagnostics)
	}
	if len(diagnostics) != 1 {
		t.Errorf("want the refusal once, got %v", diagnostics)
	}
}

func TestFreedomsTransportMustStartWithTCP(t *testing.T) {
	cases := map[string]string{
		`[{"type": "udp"}]`:      "cannot carry freedom's TCP traffic",
		`[{"type": "fragment"}]`: `"fragment" is an overlay and needs a connection below it`,
	}
	for transport, want := range cases {
		diagnostics, failed := loadAndCheck(t, freedomWithTransport(transport))
		if !failed || !find(diagnostics, "outbounds[0].transport[0]", want) {
			t.Errorf("%s: want %q, got %v", transport, want, diagnostics)
		}
	}
}

func TestFreedomsOldTCPSettingsAreRefused(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"protocol": {"type": "freedom", "settings": {"tcp": {"keep_alive": "15s"}}}`, 1)
	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "outbounds[0].protocol.settings.tcp", "freedom no longer takes tcp here") {
		t.Errorf("settings.tcp was not refused clearly: %v", diagnostics)
	}
}

func TestATransportWithNoServerSideIsRefusedOnAnInbound(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5"`,
		`"transport": [{"type": "tcp"}, {"type": "fragment"}],
    "protocol": {"type": "socks5"`, 1)
	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "inbounds[0].transport[1]", `"fragment" has no server side`) {
		t.Errorf("fragment on an inbound was not refused: %v", diagnostics)
	}
}

// noServerSideTransport stands for a program's own technique: the list of
// transports that work with freedom must come from the registry, not a list
// in core.
type noServerSideTransport struct{}

func (noServerSideTransport) Validate() error    { return nil }
func (noServerSideTransport) Warnings() []string { return nil }
func (noServerSideTransport) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "decoy", Description: "test"}
}
func (noServerSideTransport) TransportInfo() spec.TransportInfo {
	return spec.TransportInfo{NeedsNoServerSide: true}
}
func (noServerSideTransport) NewStreamOverlayClient() (spec.StreamOverlayClient, error) {
	return nil, nil
}
func (noServerSideTransport) NewStreamOverlayServer() (spec.StreamOverlayServer, error) {
	return nil, nil
}

func TestTheFreedomListComesFromTheRegistry(t *testing.T) {
	registered := modules(t)
	if err := registered.Transports().RegisterOverlayStream(func() spec.OverlayStreamTransportModule { return noServerSideTransport{} }); err != nil {
		t.Fatal(err)
	}
	_, diagnostics := config.Parse([]byte(freedomWithTransport(`[{"type": "tcp"}, {"type": "tls"}]`)), registered)
	if !find(diagnostics, "outbounds[0].transport[1]", "Transports that work with freedom: tcp, decoy, fragment") {
		t.Errorf("the registered module is missing from the list: %v", diagnostics)
	}

	if _, diagnostics := config.Parse([]byte(freedomWithTransport(`[{"type": "tcp"}, {"type": "decoy"}, {"type": "fragment"}]`)), registered); diagnostics.HasErrors() {
		t.Errorf("a registered module that needs no server side was refused: %v", diagnostics)
	}
}

func TestTheOldDatagramKeyIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"datagram": [{"type": "udp"}], "protocol": {"type": "freedom"}`, 1)

	if _, failed := loadAndCheck(t, document); !failed {
		t.Error("an outbound with a datagram key was accepted")
	}
}

func TestAnOutboundNeedsAProtocol(t *testing.T) {
	document := strings.Replace(goodDocument, `"protocol": {"type": "freedom"}`, `"transport": []`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "outbounds[0].protocol", "type") {
		t.Errorf("an outbound with no protocol was not refused with its path: %v", diagnostics)
	}
}

func TestDuplicateTagsAreRefused(t *testing.T) {
	document := `{
	  "version": 1,
	  "inbounds": [
	    {"tag": "same", "listen": "127.0.0.1", "port": 1080, "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}},
	    {"tag": "same", "listen": "127.0.0.1", "port": 1081, "transport": [{"type": "tcp"}], "protocol": {"type": "socks5"}}
	  ],
	  "outbounds": [{"tag": "freedom", "transport": [{"type": "tcp"}]}]
	}`

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("two inbounds with the same tag were accepted")
	}
	if !find(diagnostics, "inbounds[1].tag", "already used") {
		t.Errorf("diagnostics do not report the duplicate: %v", diagnostics)
	}
}

func TestUDPInboundNeedsADatagramProxy(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"listen": "127.0.0.1",`,
		`"listen": "127.0.0.1", "network": "udp",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a udp inbound with a stream proxy was accepted")
	}
	if !find(diagnostics, "inbounds[0].protocol", "datagram proxy") {
		t.Errorf("diagnostics do not explain the datagram proxy requirement: %v", diagnostics)
	}
}

func TestUnknownNetworkIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"listen": "127.0.0.1",`,
		`"listen": "127.0.0.1", "network": "sctp",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("an unknown network was accepted")
	}
	if !find(diagnostics, "inbounds[0].network", "sctp") {
		t.Errorf("diagnostics do not name the bad network: %v", diagnostics)
	}
}

// ---------- warnings ----------

// Each component's advisory findings, prefixed with where it sits.
func TestComponentWarningsCarryTheirPath(t *testing.T) {
	document := strings.Replace(strings.Replace(goodDocument,
		`"settings": {"users": [{"name": "u", "password": "p"}]}`, `"settings": {}`, 1),
		`"listen": "127.0.0.1"`, `"listen": "0.0.0.0"`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("an unauthenticated proxy is legal: %v", diagnostics.Err())
	}
	if !find(diagnostics, "inbounds[0].protocol.settings", "no users") {
		t.Errorf("the module's warning did not reach the diagnostics with a path: %v", diagnostics)
	}
}

func TestSilentLoggingIsWarnedAbout(t *testing.T) {
	document := strings.Replace(goodDocument, `"log": {"level": "info", "outputs": [{"type": "stderr"}]},`, "", 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("a config file with no log block is legal: %v", diagnostics.Err())
	}
	if !find(diagnostics, "log", "nothing will be logged") {
		t.Errorf("silent logging produced no warning: %v", diagnostics)
	}
}

// ---------- nat ----------

func TestUDPPolicyDecodes(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"protocol": {"type": "freedom", "settings": {"nat": {"mapping": "address-dependent", "filtering": "address-dependent", "idle_timeout": "30s"}}}`, 1)

	registered := modules(t)
	configuration, diagnostics := config.Parse([]byte(document), registered)
	requireNoErrors(t, diagnostics)

	policy := configuration.Outbounds[0].Freedom.NAT
	if policy == nil {
		t.Fatal("the nat settings did not reach the config")
	}
	if policy.Mapping() != nat.AddressDependentMapping {
		t.Errorf("mapping = %v, want address-dependent", policy.Mapping())
	}
	if policy.Filtering() != nat.AddressDependentFiltering {
		t.Errorf("filtering = %v, want address-dependent", policy.Filtering())
	}
	if policy.IdleTimeout() != 30*time.Second {
		t.Errorf("idle timeout = %v, want 30s", policy.IdleTimeout())
	}
}

// The least secure filtering mode lets an off-path attacker inject packets.
func TestEndpointIndependentFilteringIsWarnedAbout(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"protocol": {"type": "freedom", "settings": {"nat": {"filtering": "endpoint-independent"}}}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("the mode is legal: %v", diagnostics.Err())
	}
	if !find(diagnostics, "outbounds[0].protocol.settings.nat.filtering", "off-path") {
		t.Errorf("no warning for the weakest filtering mode: %v", diagnostics)
	}
}

func TestUnknownUDPBehaviourIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"protocol": {"type": "freedom", "settings": {"nat": {"mapping": "whatever"}}}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("an unknown mapping behaviour was accepted")
	}
	if !find(diagnostics, "outbounds[0].protocol.settings.nat.mapping", "whatever") {
		t.Errorf("diagnostics do not name the bad behaviour: %v", diagnostics)
	}
}

// ---------- log ----------

func TestLogOutputsBecomeSinks(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"outputs": [{"type": "stderr"}]`,
		`"outputs": [{"type": "stderr"}, {"type": "memory", "capacity": 64}]`, 1)

	registered := modules(t)
	configuration, diagnostics := config.Parse([]byte(document), registered)
	requireNoErrors(t, diagnostics)

	if len(configuration.Log.Outputs) != 2 {
		t.Errorf("got %d outputs, want 2", len(configuration.Log.Outputs))
	}
}

func TestUnknownLogOutputIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument, `{"type": "stderr"}`, `{"type": "teletype"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("an unknown log output was accepted")
	}
	if !find(diagnostics, "log.outputs[0]", "teletype") {
		t.Errorf("diagnostics do not name the bad output: %v", diagnostics)
	}
}

func TestFileOutputNeedsAPath(t *testing.T) {
	document := strings.Replace(goodDocument, `{"type": "stderr"}`, `{"type": "file"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed {
		t.Fatal("a file output with no path was accepted")
	}
	if !find(diagnostics, "log.outputs[0]", "path") {
		t.Errorf("diagnostics do not name the missing field: %v", diagnostics)
	}
}

// ---------- removed fields ----------

func TestInboundSniffingPointsToTheSniffAction(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "socks5",`,
		`"sniffing": {"sniffers": [{"type": "tls"}]}, "protocol": {"type": "socks5",`, 1)
	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "inbounds[0].sniffing", `"type": "sniff"`) {
		t.Errorf("inbound sniffing was not refused with a pointer to the sniff action: %v", diagnostics)
	}
}

func TestBlockOutboundPointsToTheBlockAction(t *testing.T) {
	document := strings.Replace(goodDocument, `"protocol": {"type": "freedom"}`, `"protocol": {"type": "block"}`, 1)
	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "outbounds[0].protocol.type", "routing action") {
		t.Errorf("a block outbound was not refused with a pointer to the block action: %v", diagnostics)
	}
}

// ---------- timeouts ----------

func TestTimeoutsDecodeBesideTheirComponents(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"listen": "127.0.0.1",`,
		`"listen": "127.0.0.1", "tcp_idle_timeout": "2m", "udp_session_idle_timeout": "90s",`, 1)

	configuration, diagnostics := config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))

	inbound := configuration.Inbounds[0]
	if inbound.TCPIdleTimeout == nil || *inbound.TCPIdleTimeout != 2*time.Minute ||
		inbound.UDPSessionIdleTimeout == nil || *inbound.UDPSessionIdleTimeout != 90*time.Second {
		t.Errorf("inbound timeouts = %v, %v; want 2m, 90s", inbound.TCPIdleTimeout, inbound.UDPSessionIdleTimeout)
	}
}

func TestUDPResolveSettingsPointToTheDNSSection(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"protocol": {"type": "freedom", "settings": {"udp": {"resolve_timeout": "3s", "resolve_cache_ttl": "1m"}}}`, 1)
	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "outbounds[0].protocol.settings.udp", "moved to the dns section") {
		t.Errorf("udp's resolve settings were not refused with a pointer to dns: %v", diagnostics)
	}
}

func TestANegativeUDPSessionIdleTimeoutIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"listen": "127.0.0.1",`,
		`"listen": "127.0.0.1", "udp_session_idle_timeout": "-1s",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "inbounds[0].udp_session_idle_timeout", "never be released") {
		t.Errorf("a negative UDP session idle timeout was not refused with its path: %v", diagnostics)
	}
}

func withInboundSettings(settings string) string {
	return strings.Replace(goodDocument, `"listen": "127.0.0.1",`, `"listen": "127.0.0.1", `+settings+`,`, 1)
}

func TestTheUDPRoutingCacheSettingsAreAbsentUntilWritten(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(goodDocument), modules(t))
	requireNoErrors(t, diagnostics)
	if inbound := configuration.Inbounds[0]; inbound.UDPRoutingCacheEntryIdleTimeout != nil || inbound.UDPRoutingCacheMaxEntriesPerSession != nil {
		t.Errorf("defaults = %v and %v, want both absent (nil), which means the built-in defaults",
			inbound.UDPRoutingCacheEntryIdleTimeout, inbound.UDPRoutingCacheMaxEntriesPerSession)
	}

	document := withInboundSettings(`"udp_routing_cache_entry_idle_timeout": "20s", "udp_routing_cache_max_entries_per_session": 300`)
	configuration, diagnostics = config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))
	inbound := configuration.Inbounds[0]
	if inbound.UDPRoutingCacheEntryIdleTimeout == nil || *inbound.UDPRoutingCacheEntryIdleTimeout != 20*time.Second {
		t.Errorf("udp_routing_cache_entry_idle_timeout = %v, want 20s", inbound.UDPRoutingCacheEntryIdleTimeout)
	}
	if inbound.UDPRoutingCacheMaxEntriesPerSession == nil || *inbound.UDPRoutingCacheMaxEntriesPerSession != 300 {
		t.Errorf("udp_routing_cache_max_entries_per_session = %v, want 300", inbound.UDPRoutingCacheMaxEntriesPerSession)
	}
}

// Zero never means the default: only leaving the key out does.
func TestZeroAndNegativeUDPRoutingCacheSettingsAreRefused(t *testing.T) {
	for key, values := range map[string][]string{
		"udp_routing_cache_entry_idle_timeout":      {`"0s"`, `"-1s"`},
		"udp_routing_cache_max_entries_per_session": {`0`, `-1`},
	} {
		for _, value := range values {
			diagnostics, failed := loadAndCheck(t, withInboundSettings(`"`+key+`": `+value))
			if !failed || !find(diagnostics, "inbounds[0]."+key, "must be greater than zero") {
				t.Errorf("%s = %s was not refused with its path: %v", key, value, diagnostics)
			}
		}
	}
}

func TestARoutingCacheEntryIdleTimeoutLongerThanTheSessionIdleTimeoutIsWarnedAbout(t *testing.T) {
	const key = "inbounds[0].udp_routing_cache_entry_idle_timeout"
	diagnostics, failed := loadAndCheck(t, withInboundSettings(`"udp_session_idle_timeout": "30s", "udp_routing_cache_entry_idle_timeout": "2m"`))
	if failed || !find(diagnostics, key, "longer than udp_session_idle_timeout") {
		t.Errorf("the longer entry idle time was not warned about (failed=%v): %v", failed, diagnostics)
	}

	diagnostics, _ = loadAndCheck(t, withInboundSettings(`"udp_routing_cache_entry_idle_timeout": "2m"`))
	if find(diagnostics, key, "longer than udp_session_idle_timeout") {
		t.Errorf("2m was warned about against the default 5m session idle time: %v", diagnostics)
	}

	diagnostics, _ = loadAndCheck(t, withInboundSettings(`"udp_routing_cache_entry_idle_timeout": "10m"`))
	if !find(diagnostics, key, "longer than udp_session_idle_timeout") {
		t.Errorf("10m against the default 5m session idle time was not warned about: %v", diagnostics)
	}
}

func TestTheOldUDPLiveConnectionsKeysAreUnknownNow(t *testing.T) {
	_, failed := loadAndCheck(t, withInboundSettings(`"udp_session_live_connections_idle_timeout": "30s"`))
	if !failed {
		t.Error("the removed inbound live setting was accepted")
	}
}

// The old keys beside the components are refused with where they moved.
func TestTheMovedTimeoutKeysPointToTheirNewPlace(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"listen": "127.0.0.1",`,
		`"listen": "127.0.0.1", "handshake_timeout": "5s",`, 1)
	document = strings.Replace(document,
		`"tag": "freedom",`,
		`"tag": "freedom", "dial_timeout": "5s", "handshake_timeout": "5s",`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	for _, want := range []struct{ location, text string }{
		{"inbounds[0].handshake_timeout", "each layer's own settings"},
		{"outbounds[0].dial_timeout", "tcp's settings"},
		{"outbounds[0].handshake_timeout", "each layer's own settings"},
	} {
		if !failed || !find(diagnostics, want.location, want.text) {
			t.Errorf("%s was not refused with where it moved: %v", want.location, diagnostics)
		}
	}
}

func TestATimeoutInsideALayersSettingsIsAccepted(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"transport": [{"type": "tcp", "settings": {"dial_timeout": "5s"}}], "protocol": {"type": "freedom"}`, 1)

	if diagnostics, failed := loadAndCheck(t, document); failed {
		t.Errorf("tcp's dial_timeout was refused: %v", diagnostics)
	}
}

func TestANegativeResolveTimeoutIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"protocol": {"type": "freedom"}`,
		`"protocol": {"type": "freedom", "settings": {"udp": {"resolve_timeout": "-1s"}}}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "outbounds[0].protocol.settings.udp", "resolve_timeout") {
		t.Errorf("a negative resolve timeout was not refused with its path: %v", diagnostics)
	}
}

// Trojan is registered once and fills a stream kind and a datagram-over-stream
// kind; one protocol block fills both, on either side, with one value.
func TestTrojanFillsBothKindsAndTakesAFallback(t *testing.T) {
	registered := modules(t)
	configuration, diagnostics := config.Parse([]byte(`{
  "version": 1,
  "inbounds": [{
    "tag": "trojan-in", "listen": "127.0.0.1", "port": 443,
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "trojan", "settings": {"users": [{"name": "alice", "password": "p"}]}},
    "fallback": {"server": "127.0.0.1", "server_port": 8080}
  }],
  "outbounds": [{
    "tag": "trojan-out", "server": "example.com", "server_port": 443,
    "transport": [{"type": "tcp"}],
    "protocol": {"type": "trojan", "settings": {"password": "p"}}
  }]
}`), registered)
	requireNoErrors(t, diagnostics)

	in, out := configuration.Inbounds[0], configuration.Outbounds[0]
	if in.StreamProxy == nil || in.DatagramOverStreamProxy == nil {
		t.Error("the inbound is missing one of trojan's kinds")
	}
	if out.Proxy == nil || out.DatagramOverStreamProxy == nil {
		t.Error("the outbound is missing one of trojan's kinds")
	}
	if any(in.StreamProxy) != any(in.DatagramOverStreamProxy) || any(out.Proxy) != any(out.DatagramOverStreamProxy) {
		t.Error("a protocol that fills two kinds was built as two values")
	}
	if in.Fallback.String() != "127.0.0.1:8080" {
		t.Errorf("Fallback = %s", in.Fallback)
	}
}

func TestTwoInboundsOfOneProtocolKeepTheirOwnSettings(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(`{
  "version": 1,
  "inbounds": [
    {"tag": "a", "listen": "127.0.0.1", "port": 443, "transport": [{"type": "tcp"}],
     "protocol": {"type": "trojan", "settings": {"users": [{"name": "alice", "password": "p"}]}}},
    {"tag": "b", "listen": "127.0.0.1", "port": 444, "transport": [{"type": "tcp"}],
     "protocol": {"type": "trojan", "settings": {"users": [{"name": "bob", "password": "q"}]}}}
  ],
  "outbounds": [{"tag": "freedom", "protocol": {"type": "freedom"}}]
}`), modules(t))
	requireNoErrors(t, diagnostics)

	first := configuration.Inbounds[0].StreamProxy.(*trojan.Proxy)
	second := configuration.Inbounds[1].StreamProxy.(*trojan.Proxy)
	if first == second || first.Users[0].Name != "alice" || second.Users[0].Name != "bob" {
		t.Errorf("the inbounds share settings: %+v and %+v", first.Users, second.Users)
	}
}

func TestASettingsErrorOfAProtocolFillingTwoKindsIsReportedOnce(t *testing.T) {
	_, diagnostics := config.Parse([]byte(`{
  "version": 1,
  "inbounds": [{"tag": "a", "listen": "127.0.0.1", "port": 443, "transport": [{"type": "tcp"}],
     "protocol": {"type": "trojan", "settings": {"nonsense": true}}}],
  "outbounds": [{"tag": "freedom", "protocol": {"type": "freedom"}}]
}`), modules(t))

	reported := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Location == "inbounds[0].protocol.settings" {
			reported++
		}
	}
	if reported != 1 {
		t.Errorf("the settings error was reported %d times:\n%v", reported, diagnostics)
	}
}

func TestAStreamOnlyProtocolHasNoDatagramOverStreamKind(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(goodDocument), modules(t))
	requireNoErrors(t, diagnostics)
	if configuration.Inbounds[0].DatagramOverStreamProxy != nil {
		t.Error("socks5 was given a datagram-over-stream kind it never registered")
	}
}

func TestAFallbackNeedsAPort(t *testing.T) {
	document := strings.Replace(goodDocument, `"port": 1080,`, `"port": 1080, "fallback": {"server": "127.0.0.1"},`, 1)
	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !strings.Contains(diagnostics.Err().Error(), "fallback") {
		t.Errorf("a fallback without a port was accepted: %v", diagnostics.Err())
	}
}

// ---------- the special inbound protocol mixed ----------

const mixedProtocol = `"protocol": {"type": "mixed", "settings": {"users": [{"name": "u", "password": "p"}], "socks5": {"handshake_timeout": "5s"}, "http": {"handshake_timeout": "5s"}}}`

func mixedDocument(protocol string) string {
	return strings.Replace(goodDocument,
		`"protocol": {"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}}`, protocol, 1)
}

func TestAMixedInboundDecodesBothHalves(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(mixedDocument(mixedProtocol)), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))

	inbound := configuration.Inbounds[0]
	if inbound.Mixed == nil || inbound.StreamProxy != nil {
		t.Fatalf("inbound = %+v, want only Mixed", inbound)
	}
	if inbound.Mixed.SOCKS5.Info().Name != "socks5" || inbound.Mixed.HTTP.Info().Name != "http" {
		t.Errorf("halves = %q and %q", inbound.Mixed.SOCKS5.Info().Name, inbound.Mixed.HTTP.Info().Name)
	}
	if socks5Half := inbound.Mixed.SOCKS5.(*socks5.Proxy); socks5Half.HandshakeTimeout == nil || !socks5Half.UDPRelay {
		t.Errorf("socks5 half = %+v, want its settings over the module's defaults", socks5Half.Settings)
	}
	if len(inbound.Mixed.Users) != 1 {
		t.Errorf("Mixed.Users = %s, want the one user written at mixed's level", inbound.Mixed.Users)
	}
}

// mixed is one inbound with one list of users, so the halves are not warned
// about having none, and the password that crosses the wire is.
func TestMixedUsersReplaceTheHalvesOpenProxyWarnings(t *testing.T) {
	document := strings.Replace(mixedDocument(mixedProtocol), `"listen": "127.0.0.1"`, `"listen": "0.0.0.0"`, 1)
	configuration, diagnostics := config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	checked := config.Check(configuration)
	requireNoErrors(t, checked)

	for _, path := range []string{"inbounds[0].protocol.settings.socks5", "inbounds[0].protocol.settings.http"} {
		if find(checked, path, "no users") {
			t.Errorf("a half was warned about having no users, though mixed has some: %v", checked)
		}
	}
	if !find(checked, "inbounds[0].protocol.settings.users", "recoverable form") {
		t.Errorf("no warning about the password crossing the wire: %v", checked)
	}
}

func TestAMixedInboundWithoutSettingsUsesTheModulesDefaults(t *testing.T) {
	document := strings.Replace(mixedDocument(`"protocol": {"type": "mixed"}`), `"listen": "127.0.0.1"`, `"listen": "0.0.0.0"`, 1)
	configuration, diagnostics := config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	checked := config.Check(configuration)
	requireNoErrors(t, checked)

	for _, path := range []string{"inbounds[0].protocol.settings.socks5", "inbounds[0].protocol.settings.http"} {
		if !find(checked, path, "no users, so any client") {
			t.Errorf("no warning about the open half at %s: %v", path, checked)
		}
	}
}

func TestAMixedHalfsErrorIsReportedAtItsPath(t *testing.T) {
	for path, protocol := range map[string]string{
		"inbounds[0].protocol.settings":        `"protocol": {"type": "mixed", "settings": {"socks": {}}}`,
		"inbounds[0].protocol.settings.socks5": `"protocol": {"type": "mixed", "settings": {"socks5": {"usernam": "u"}}}`,
		"inbounds[0].protocol.settings.http":   `"protocol": {"type": "mixed", "settings": {"http": {"username": "u"}}}`,
	} {
		diagnostics, failed := loadAndCheck(t, mixedDocument(protocol))
		if !failed || !find(diagnostics, path, "") {
			t.Errorf("%s: not refused at that path: %v", path, diagnostics)
		}
	}
}

func TestStrictFalseAcceptsUnknownMixedSettings(t *testing.T) {
	document := strings.Replace(mixedDocument(`"protocol": {"type": "mixed", "settings": {"socks": {}, "http": {"usernam": "u"}}}`),
		`"version": 1,`, `"version": 1, "strict": false,`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("strict:false still refused an unknown field: %v", diagnostics.Err())
	}
}

func TestAUDPInboundCannotBeMixed(t *testing.T) {
	document := mixedDocument(`"network": "udp", "protocol": {"type": "mixed"}`)
	document = strings.Replace(document, `"transport": [{"type": "tcp"}]`, `"transport": [{"type": "udp"}]`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "inbounds[0].protocol.type", "mixed") {
		t.Errorf("mixed on a udp inbound was not refused at protocol.type: %v", diagnostics)
	}
}

func TestAMixedInboundNeedsBothModulesRegistered(t *testing.T) {
	withoutHTTP := registry.New()
	if err := errors.Join(
		withoutHTTP.Transports().RegisterBaseStream(tcp.NewBaseStreamTransportModule),
		withoutHTTP.ProxyProtocols().RegisterStream(socks5.NewProxyModule),
	); err != nil {
		t.Fatal(err)
	}
	document := mixedDocument(`"protocol": {"type": "mixed"}`)

	_, diagnostics := config.Parse([]byte(document), withoutHTTP)
	if !diagnostics.HasErrors() || !find(diagnostics, "inbounds[0].protocol.settings.http", "http") {
		t.Errorf("a registry without http was not reported at the http half: %v", diagnostics)
	}
}

func TestAnOutboundCannotBeMixed(t *testing.T) {
	document := strings.Replace(goodDocument, `"protocol": {"type": "freedom"}`, `"protocol": {"type": "mixed"}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "outbounds[0].protocol.type", "inbound protocol") {
		t.Errorf("mixed on an outbound was not refused at protocol.type: %v", diagnostics)
	}
}

// ---------- max_tcp_connections ----------

func TestMaxTCPConnectionsIsDecoded(t *testing.T) {
	document := strings.Replace(goodDocument, `"port": 1080,`, `"port": 1080, "max_tcp_connections": 500,`, 1)

	configuration, diagnostics := config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))
	if got := configuration.Inbounds[0].MaxTCPConnections; got == nil || *got != 500 {
		t.Errorf("MaxTCPConnections = %v, want 500", got)
	}
}

func TestZeroAndNegativeLimitsAreRefused(t *testing.T) {
	for _, key := range []string{"max_tcp_connections", "max_udp_sessions"} {
		for _, value := range []string{"0", "-1"} {
			document := strings.Replace(goodDocument, `"port": 1080,`, `"port": 1080, "`+key+`": `+value+`,`, 1)
			diagnostics, failed := loadAndCheck(t, document)
			if !failed || !find(diagnostics, "inbounds[0]."+key, "greater than zero") {
				t.Errorf("%s = %s was not refused at its path: %v", key, value, diagnostics)
			}
		}
	}
}

func TestMaxTCPConnectionsOnAUDPInboundIsRefused(t *testing.T) {
	document := strings.Replace(goodDocument, `"port": 1080,`, `"port": 1080, "network": "udp", "max_tcp_connections": 5,`, 1)

	diagnostics, _ := loadAndCheck(t, document)
	if !find(diagnostics, "inbounds[0].max_tcp_connections", "udp") {
		t.Errorf("max_tcp_connections on a udp inbound was not refused at its path: %v", diagnostics)
	}
}

func TestMaxUDPSessionsIsDecoded(t *testing.T) {
	document := strings.Replace(goodDocument, `"port": 1080,`, `"port": 1080, "max_udp_sessions": 8192,`, 1)

	configuration, diagnostics := config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))
	if got := configuration.Inbounds[0].MaxUDPSessions; got == nil || *got != 8192 {
		t.Errorf("MaxUDPSessions = %v, want 8192", got)
	}
}

func TestNoLimitIsSetUnlessTheConfigAsksForOne(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(goodDocument), modules(t))
	requireNoErrors(t, diagnostics)
	if inbound := configuration.Inbounds[0]; inbound.MaxTCPConnections != nil || inbound.MaxUDPSessions != nil {
		t.Errorf("limits = %v TCP connections, %v UDP sessions, want both absent (no limit)", inbound.MaxTCPConnections, inbound.MaxUDPSessions)
	}
}

func TestTheOldMaxConnectionsKeyIsRefusedWithItsNewName(t *testing.T) {
	document := strings.Replace(goodDocument, `"port": 1080,`, `"port": 1080, "max_connections": 500,`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "inbounds[0].max_connections", "max_tcp_connections") {
		t.Errorf("max_connections was not refused with its new name: %v", diagnostics)
	}
}

// ---------- application protocols across a stack ----------

// withOutboundStack replaces the freedom outbound with a Trojan outbound over
// the given transport stack.
func withOutboundStack(stack string) string {
	return strings.Replace(goodDocument,
		`"tag": "freedom",
    "protocol": {"type": "freedom"}`,
		`"tag": "proxied",
    "server": "proxy.example.com",
    "server_port": 443,
    "transport": `+stack+`,
    "protocol": {"type": "trojan", "settings": {"password": "p"}}`, 1)
}

func TestTLSMayOfferOnlyWhatTheTransportAboveSpeaks(t *testing.T) {
	const alpnPath = "outbounds[0].transport[1].settings.alpn"
	for name, tc := range map[string]struct {
		stack   string
		refused bool
	}{
		"chrome's list under ws": {
			`[{"type": "tcp"}, {"type": "tls", "settings": {"fingerprint": "chrome"}}, {"type": "ws"}]`, true},
		"randomized under httpupgrade": {
			`[{"type": "tcp"}, {"type": "tls", "settings": {"fingerprint": "randomized"}}, {"type": "httpupgrade"}]`, true},
		"h2 and http/1.1 under ws": {
			`[{"type": "tcp"}, {"type": "tls", "settings": {"alpn": ["h2", "http/1.1"]}}, {"type": "ws"}]`, true},
		"h2 under httpupgrade": {
			`[{"type": "tcp"}, {"type": "tls", "settings": {"alpn": ["h2"]}}, {"type": "httpupgrade"}]`, true},
		"http/1.1 under ws": {
			`[{"type": "tcp"}, {"type": "tls", "settings": {"fingerprint": "chrome", "alpn": ["http/1.1"]}}, {"type": "ws"}]`, false},
		"the default fingerprint under ws": {
			`[{"type": "tcp"}, {"type": "tls"}, {"type": "ws"}]`, true},
		"no fingerprint under ws": {
			`[{"type": "tcp"}, {"type": "tls", "settings": {"fingerprint": "none"}}, {"type": "ws"}]`, false},
		"chrome's list with nothing above": {
			`[{"type": "tcp"}, {"type": "tls", "settings": {"fingerprint": "chrome"}}]`, false},
	} {
		diagnostics, failed := loadAndCheck(t, withOutboundStack(tc.stack))
		if failed != tc.refused {
			t.Errorf("%s: refused %v, want %v: %v", name, failed, tc.refused, diagnostics)
			continue
		}
		if tc.refused && !find(diagnostics, alpnPath, `set alpn to ["http/1.1"]`) {
			t.Errorf("%s: no error naming the fix at %s: %v", name, alpnPath, diagnostics)
		}
	}
}

// A server's offer is its alpn alone: the default fingerprint is a client's.
func TestAnInboundTLSServerWithoutAlpnPassesUnderWS(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5"`,
		`"transport": [{"type": "tcp"}, {"type": "tls", "settings": {"certificates": [{"certificate": "c", "key": "k"}]}}, {"type": "ws"}],
    "protocol": {"type": "socks5"`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Errorf("an inbound tls without alpn under ws was refused: %v", diagnostics.Err())
	}
}

func TestAnInboundTLSServerIsCheckedToo(t *testing.T) {
	document := strings.Replace(goodDocument,
		`"transport": [{"type": "tcp"}],
    "protocol": {"type": "socks5"`,
		`"transport": [{"type": "tcp"}, {"type": "tls", "settings": {"alpn": ["h2", "http/1.1"], "certificates": [{"certificate": "c", "key": "k"}]}}, {"type": "ws"}],
    "protocol": {"type": "socks5"`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if !failed || !find(diagnostics, "inbounds[0].transport[1].settings.alpn", "ws above it can speak only") {
		t.Errorf("an inbound tls offering h2 under ws was not refused: %v", diagnostics)
	}
}

// What depends on where an inbound listens is only said where others can reach it.
func TestAnOpenInboundOnLoopbackIsNotWarnedAbout(t *testing.T) {
	document := strings.Replace(goodDocument, `"settings": {"users": [{"name": "u", "password": "p"}]}`, `"settings": {}`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("an unauthenticated proxy is legal: %v", diagnostics.Err())
	}
	if find(diagnostics, "inbounds[0].protocol.settings", "no users") {
		t.Errorf("a loopback inbound was warned about: %v", diagnostics)
	}
}

func TestWebSocketReadingAHeaderWithoutVerificationIsWarnedAboutOnAnOpenAddress(t *testing.T) {
	document := strings.Replace(strings.Replace(goodDocument,
		`"transport": [{"type": "tcp"}]`, `"transport": [{"type": "tcp"}, {"type": "ws"}]`, 1),
		`"listen": "127.0.0.1"`, `"listen": "0.0.0.0"`, 1)

	diagnostics, failed := loadAndCheck(t, document)
	if failed {
		t.Fatalf("legal config refused: %v", diagnostics.Err())
	}
	if !find(diagnostics, "inbounds[0].transport[1].settings", "original_source_ip") {
		t.Errorf("no warning about the unverified header: %v", diagnostics)
	}

	verified := strings.Replace(document, `{"type": "ws"}`, `{"type": "ws", "settings": {"original_source_ip": {"verification": {"trusted_proxy_addresses": ["10.0.0.2"]}}}}`, 1)
	diagnostics, _ = loadAndCheck(t, verified)
	if find(diagnostics, "inbounds[0].transport[1].settings", "original_source_ip") {
		t.Errorf("a verified setting was warned about: %v", diagnostics)
	}
}

func TestTheUDPDatagramSizeSettingsDecodeAndAreChecked(t *testing.T) {
	document := strings.Replace(goodDocument, `"listen": "127.0.0.1",`, `"listen": "127.0.0.1", "udp_max_datagram_size": 4096,`, 1)
	document = strings.Replace(document, `"protocol": {"type": "freedom"}`, `"protocol": {"type": "freedom", "settings": {"nat": {"max_datagram_size": 2048}}}`, 1)
	document = strings.Replace(document, `"settings": {"users": [{"name": "u", "password": "p"}]}`, `"settings": {"users": [{"name": "u", "password": "p"}], "udp_max_datagram_size": 4096}`, 1)
	configuration, diagnostics := config.Parse([]byte(document), modules(t))
	requireNoErrors(t, diagnostics)
	requireNoErrors(t, config.Check(configuration))
	if got := configuration.Inbounds[0].UDPMaxDatagramSize; got == nil || *got != 4096 {
		t.Errorf("inbound udp_max_datagram_size = %v, want 4096", got)
	}
	if got := configuration.Outbounds[0].Freedom.NATMaxDatagramSize; got == nil || *got != 2048 {
		t.Errorf("freedom nat.max_datagram_size = %v, want 2048", got)
	}

	configuration, diagnostics = config.Parse([]byte(goodDocument), modules(t))
	requireNoErrors(t, diagnostics)
	if configuration.Inbounds[0].UDPMaxDatagramSize != nil || configuration.Outbounds[0].Freedom.NATMaxDatagramSize != nil {
		t.Error("the defaults are not absent (nil), which means the built-in default")
	}
}

func TestADatagramSizeOutOfRangeIsRefusedWithItsPath(t *testing.T) {
	for _, test := range []struct{ name, from, to, path string }{
		{"inbound negative", `"listen": "127.0.0.1",`, `"listen": "127.0.0.1", "udp_max_datagram_size": -1,`, "inbounds[0].udp_max_datagram_size"},
		{"inbound zero", `"listen": "127.0.0.1",`, `"listen": "127.0.0.1", "udp_max_datagram_size": 0,`, "inbounds[0].udp_max_datagram_size"},
		{"freedom zero", `"protocol": {"type": "freedom"}`, `"protocol": {"type": "freedom", "settings": {"nat": {"max_datagram_size": 0}}}`, "outbounds[0].protocol.settings.nat.max_datagram_size"},
		{"inbound too big", `"listen": "127.0.0.1",`, `"listen": "127.0.0.1", "udp_max_datagram_size": 70000,`, "inbounds[0].udp_max_datagram_size"},
		{"freedom too big", `"protocol": {"type": "freedom"}`, `"protocol": {"type": "freedom", "settings": {"nat": {"max_datagram_size": 70000}}}`, "outbounds[0].protocol.settings.nat.max_datagram_size"},
	} {
		diagnostics, failed := loadAndCheck(t, strings.Replace(goodDocument, test.from, test.to, 1))
		if !failed || !find(diagnostics, test.path, "") {
			t.Errorf("%s: not refused with its path %s: %v", test.name, test.path, diagnostics)
		}
	}

	document := strings.Replace(goodDocument, `"settings": {"users": [{"name": "u", "password": "p"}]}`, `"settings": {"users": [{"name": "u", "password": "p"}], "udp_max_datagram_size": 70000}`, 1)
	if _, failed := loadAndCheck(t, document); !failed {
		t.Error("a socks5 udp_max_datagram_size over the largest datagram was accepted")
	}
}

func TestADatagramSizeBelowADNSMessageIsWarnedAbout(t *testing.T) {
	document := strings.Replace(goodDocument, `"listen": "127.0.0.1",`, `"listen": "127.0.0.1", "udp_max_datagram_size": 100,`, 1)
	diagnostics, failed := loadAndCheck(t, document)
	if failed || !find(diagnostics, "inbounds[0].udp_max_datagram_size", "512") {
		t.Errorf("a 100 byte limit was not warned about (failed=%v): %v", failed, diagnostics)
	}
}
