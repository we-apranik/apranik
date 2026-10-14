package config_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/we-apranik/apranik/config"
)

// An outbound with a proxy and a muxcool member; protocol is a JSON object.
func outboundWithMuxCool(protocol, muxCool string) string {
	member := ""
	if muxCool != "" {
		member = `, "muxcool": ` + muxCool
	}
	return fmt.Sprintf(`{
  "version": 1,
  "outbounds": [{
    "tag": "out", "server": "203.0.113.5", "server_port": 443,
    "transport": [{"type": "tcp"}],
    "protocol": %s%s
  }]
}`, protocol, member)
}

const vlessProtocol = `{"type": "vless", "settings": {"uuid": "b831381d-6324-4d53-ad4f-8cda48b30811"}}`
const trojanProtocol = `{"type": "trojan", "settings": {"password": "secret"}}`

func TestMuxCoolAbsentOrOnlyUDPIsAccepted(t *testing.T) {
	for name, muxCool := range map[string]string{
		"absent":              "",
		"empty":               `{}`,
		"udp on":              `{"udp": {"enabled": true, "xudp": {"enabled": true}}}`,
		"xudp empty":          `{"udp": {"xudp": {}}}`,
		"global_id on":        `{"udp": {"xudp": {"global_id": {"enabled": true}}}}`,
		"global_id off":       `{"udp": {"xudp": {"global_id": {"enabled": false}}}}`,
		"udp off":             `{"udp": {"enabled": false}}`,
		"tcp written as off":  `{"tcp": {"enabled": false}}`,
		"udp off, xudp false": `{"udp": {"enabled": false, "xudp": {"enabled": false}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, muxCool))
			if failed {
				t.Fatalf("diagnostics = %v, want none", diagnostics)
			}
		})
	}
}

func TestMuxCoolRefusesWhatIsNotBuiltWithAMessageThatSaysSo(t *testing.T) {
	for _, test := range []struct {
		name     string
		muxCool  string
		path     string
		contains string
	}{
		{"tcp on", `{"tcp": {"enabled": true}}`, "outbounds[0].muxcool.tcp.enabled", "TCP over Mux.Cool is not built yet"},
		{"tcp concurrency", `{"tcp": {"concurrency": 8}}`, "outbounds[0].muxcool.tcp.concurrency", "not built yet"},
		{"tcp max_total", `{"tcp": {"max_total": 128}}`, "outbounds[0].muxcool.tcp.max_total", "not built yet"},
		{"udp concurrency", `{"udp": {"concurrency": 8}}`, "outbounds[0].muxcool.udp.concurrency", "not built yet"},
		{"plain Mux.Cool UDP", `{"udp": {"xudp": {"enabled": false}}}`, "outbounds[0].muxcool.udp.xudp.enabled", "set muxcool.udp.enabled to false"},
		{"hold_time on a client", `{"udp": {"xudp": {"global_id": {"hold_time": "30s"}}}}`, "outbounds[0].muxcool.udp.xudp.global_id.hold_time", "hold_time is for a server"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, test.muxCool))
			if !failed || !find(diagnostics, test.path, test.contains) {
				t.Errorf("diagnostics = %v, want an error at %s containing %q", diagnostics, test.path, test.contains)
			}
		})
	}
}

func TestMuxCoolIsRefusedOnAProtocolThatCannotCarryItAtCheckTime(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, outboundWithMuxCool(trojanProtocol, `{"udp": {"enabled": true}}`))
	if !failed || !find(diagnostics, "outbounds[0].muxcool", `proxy "trojan" cannot carry muxcool`) {
		t.Errorf("diagnostics = %v, want trojan refused", diagnostics)
	}

	// Without the key, a protocol that cannot carry it is no problem.
	diagnostics, failed = loadAndCheck(t, outboundWithMuxCool(trojanProtocol, ""))
	if failed {
		t.Errorf("diagnostics = %v, want none: the defaults ask nothing of trojan", diagnostics)
	}
}

func TestMuxCoolIsRefusedOnFreedomAndGroups(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, `{
  "version": 1,
  "outbounds": [
    {"tag": "free", "protocol": {"type": "freedom"}, "muxcool": {"udp": {"enabled": true}}}
  ]
}`)
	if !failed || !find(diagnostics, "outbounds[0].muxcool", "for a proxy outbound") {
		t.Errorf("diagnostics = %v, want freedom refused", diagnostics)
	}
}

func TestMuxCoolUnknownKeysAreRefused(t *testing.T) {
	for _, muxCool := range []string{`{"enabled": true}`, `{"udp": {"idle_timeout": "15s"}}`, `{"tcp": {"max": 8}}`, `{"udp": {"xudp": {"global_id": {"timeout": "1s"}}}}`} {
		if _, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, muxCool)); !failed {
			t.Errorf("%s was accepted, want unknown keys refused", muxCool)
		}
	}
}

func TestMuxCoolIsParsedIntoTheBlueprint(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(outboundWithMuxCool(vlessProtocol, `{"udp": {"enabled": false}}`)), modules(t))
	if diagnostics.HasErrors() || configuration == nil {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	muxCool := configuration.Outbounds[0].MuxCool
	if muxCool == nil || muxCool.UDP == nil || muxCool.UDP.Enabled == nil || *muxCool.UDP.Enabled {
		t.Fatalf("MuxCool = %+v, want udp.enabled false", muxCool)
	}
	if muxCool.Options().XUDP {
		t.Error("udp off still asks the protocol for XUDP")
	}
}

// xudp was a bool before it held the Global ID; the bool is a wrong type now.
func TestXUDPIsAnObjectNotABool(t *testing.T) {
	for _, muxCool := range []string{`{"udp": {"xudp": true}}`, `{"udp": {"xudp": false}}`, `{"udp": {"xudp": {"global_id": true}}}`} {
		if _, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, muxCool)); !failed {
			t.Errorf("%s was accepted, want the bool refused", muxCool)
		}
	}
}

func TestAGlobalIDWrittenWhileXUDPIsOffIsAWarning(t *testing.T) {
	for name, muxCool := range map[string]string{
		"udp off":  `{"udp": {"enabled": false, "xudp": {"global_id": {"enabled": true}}}}`,
		"xudp off": `{"udp": {"enabled": false, "xudp": {"enabled": false, "global_id": {}}}}`,
	} {
		diagnostics, failed := loadAndCheck(t, outboundWithMuxCool(vlessProtocol, muxCool))
		if failed || !find(diagnostics, "outbounds[0].muxcool.udp.xudp.global_id", "no effect while XUDP is off") {
			t.Errorf("%s: diagnostics = %v, want only a warning at global_id", name, diagnostics)
		}
	}
}

func TestMuxCoolIsParsedIntoTheBlueprintWithTheGlobalIDSettings(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(outboundWithMuxCool(vlessProtocol, `{"udp": {"xudp": {"global_id": {"enabled": false}}}}`)), modules(t))
	if diagnostics.HasErrors() || configuration == nil {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	muxCool := configuration.Outbounds[0].MuxCool
	if !muxCool.Options().XUDP {
		t.Error("XUDP is off, want the default on")
	}
	if muxCool.XUDPGlobalIDEnabled() {
		t.Error("global_id.enabled false was not read")
	}
}

// An inbound's muxcool key: what its server accepts.

func inboundWithMuxCool(protocol, muxCool string) string {
	member := ""
	if muxCool != "" {
		member = `, "muxcool": ` + muxCool
	}
	return fmt.Sprintf(`{
  "version": 1,
  "inbounds": [{
    "tag": "in", "listen": "127.0.0.1", "port": 443,
    "transport": [{"type": "tcp"}],
    "protocol": %s%s
  }],
  "outbounds": [{"tag": "direct", "protocol": {"type": "freedom"}}]
}`, protocol, member)
}

const vlessServerProtocol = `{"type": "vless", "settings": {"users": [{"name": "alice", "uuid": "b831381d-6324-4d53-ad4f-8cda48b30811"}]}}`

func TestAnInboundTakesTheSameMuxCoolKeyAndTheHoldTime(t *testing.T) {
	for name, muxCool := range map[string]string{
		"absent":             "",
		"empty":              `{}`,
		"udp off":            `{"udp": {"enabled": false}}`,
		"global_id and hold": `{"udp": {"xudp": {"enabled": true, "global_id": {"enabled": true, "hold_time": "90s"}}}}`,
		"hold only":          `{"udp": {"xudp": {"global_id": {"hold_time": "5s"}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics, failed := loadAndCheck(t, inboundWithMuxCool(vlessServerProtocol, muxCool))
			if failed {
				t.Fatalf("diagnostics = %v, want none", diagnostics)
			}
		})
	}
}

func TestAnInboundRefusesAHoldTimeThatIsNotPositive(t *testing.T) {
	for _, holdTime := range []string{`"0s"`, `"-5s"`} {
		muxCool := `{"udp": {"xudp": {"global_id": {"hold_time": ` + holdTime + `}}}}`
		diagnostics, failed := loadAndCheck(t, inboundWithMuxCool(vlessServerProtocol, muxCool))
		if !failed || !find(diagnostics, "inbounds[0].muxcool.udp.xudp.global_id.hold_time", "must be positive") {
			t.Errorf("hold_time %s: diagnostics = %v, want it refused", holdTime, diagnostics)
		}
	}
}

func TestAHoldTimeWrittenWhileTheGlobalIDIsOffIsAWarning(t *testing.T) {
	diagnostics, failed := loadAndCheck(t, inboundWithMuxCool(vlessServerProtocol, `{"udp": {"xudp": {"global_id": {"enabled": false, "hold_time": "30s"}}}}`))
	if failed || !find(diagnostics, "inbounds[0].muxcool.udp.xudp.global_id.hold_time", "no effect while global_id.enabled is false") {
		t.Errorf("diagnostics = %v, want only a warning at hold_time", diagnostics)
	}
}

func TestAnInboundRefusesWhatIsNotBuiltWithAMessageForTheServer(t *testing.T) {
	for _, test := range []struct {
		name, muxCool, path, contains string
	}{
		{"tcp on", `{"tcp": {"enabled": true}}`, "inbounds[0].muxcool.tcp.enabled", "TCP over Mux.Cool is not built yet"},
		{"plain Mux.Cool UDP", `{"udp": {"xudp": {"enabled": false}}}`, "inbounds[0].muxcool.udp.xudp.enabled", "to take plain UDP"},
	} {
		diagnostics, failed := loadAndCheck(t, inboundWithMuxCool(vlessServerProtocol, test.muxCool))
		if !failed || !find(diagnostics, test.path, test.contains) {
			t.Errorf("%s: diagnostics = %v, want an error at %s containing %q", test.name, diagnostics, test.path, test.contains)
		}
	}
}

func TestMuxCoolIsRefusedOnAnInboundWhoseProtocolCannotCarryIt(t *testing.T) {
	socks5 := `{"type": "socks5", "settings": {"users": [{"name": "u", "password": "p"}]}}`
	diagnostics, failed := loadAndCheck(t, inboundWithMuxCool(socks5, `{"udp": {"enabled": true}}`))
	if !failed || !find(diagnostics, "inbounds[0].muxcool", `proxy "socks5" cannot carry muxcool`) {
		t.Errorf("diagnostics = %v, want socks5 refused", diagnostics)
	}

	mixed := `{"type": "mixed", "settings": {}}`
	diagnostics, failed = loadAndCheck(t, inboundWithMuxCool(mixed, `{"udp": {"enabled": true}}`))
	if !failed || !find(diagnostics, "inbounds[0].muxcool", "for an inbound with a proxy protocol") {
		t.Errorf("diagnostics = %v, want mixed refused", diagnostics)
	}
}

func TestAnInboundMuxCoolIsParsedIntoTheBlueprint(t *testing.T) {
	configuration, diagnostics := config.Parse([]byte(inboundWithMuxCool(vlessServerProtocol, `{"udp": {"xudp": {"global_id": {"hold_time": "90s"}}}}`)), modules(t))
	if diagnostics.HasErrors() || configuration == nil {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	muxCool := configuration.Inbounds[0].MuxCool
	if got := muxCool.XUDPGlobalIDHoldTime(); got != 90*time.Second {
		t.Errorf("hold time = %s, want 90s", got)
	}
	if !muxCool.XUDPGlobalIDEnabled() {
		t.Error("global_id is off, want the default on")
	}
}
